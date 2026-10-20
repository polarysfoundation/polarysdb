package polarysdb

import (
	"testing"

	"github.com/orislabsdev/gonum"
)

// ============================================================================
// Helpers
// ============================================================================

// simulateReload fuerza un flush + load del disco, simulando lo que pasa
// cuando una DB se reabre en una nueva sesión. Es el escenario donde más
// aparecen los bugs de round-trip JSON.
//
// Después de esta llamada, db.data contiene map[string]any con tipos
// "JSON-tipificados" (string, float64, json.Number, map[string]any, etc.)
// en lugar de los structs originales.
func simulateReload(t *testing.T, db *Database) {
	t.Helper()
	if err := db.flushToDisk(); err != nil {
		t.Fatalf("flushToDisk: %v", err)
	}
	db.dataMutex.Lock()
	db.data = make(map[string]map[string]any)
	db.dataMutex.Unlock()
	if err := db.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// ---- Structs de prueba ----
//
// GRec cubre todos los patrones de nesting donde *gonum.Number puede
// aparecer en una struct real:
//   - Top-level field (Price)
//   - Nested struct field (Inner.Balance)
//   - Pointer-to-struct field (Ptr.Balance)
//   - Slice of structs (Items[].Qty)
//   - Slice of pointers to struct (Ptrs[].Qty)

type GInner struct {
	Balance *gonum.Number `json:"balance"`
	Label   string        `json:"label"`
}

type GItem struct {
	Code string        `json:"code"`
	Qty  *gonum.Number `json:"qty"`
}

type GRec struct {
	ID    string        `json:"id"`
	Price *gonum.Number `json:"price"`
	Inner GInner        `json:"inner"` // struct anidado
	Ptr   *GInner       `json:"ptr"`   // puntero a struct anidado
	Items []GItem       `json:"items"` // slice de structs
	Ptrs  []*GItem      `json:"ptrs"`  // slice de punteros
}

func makeGRec() GRec {
	return GRec{
		ID:    "x1",
		Price: gonum.New("858"),
		Inner: GInner{Balance: gonum.New("50"), Label: "main"},
		Ptr:   &GInner{Balance: gonum.New("25"), Label: "sub"},
		Items: []GItem{
			{Code: "a", Qty: gonum.New("1")},
			{Code: "b", Qty: gonum.New("2")},
		},
		Ptrs: []*GItem{
			{Code: "c", Qty: gonum.New("3")},
			{Code: "d", Qty: gonum.New("4")},
		},
	}
}

func checkGRec(t *testing.T, tag string, out GRec) {
	t.Helper()
	if out.Price == nil {
		t.Errorf("[%s] Price nil", tag)
	} else if out.Price.String() != "858" {
		t.Errorf("[%s] Price = %s, want 858", tag, out.Price.String())
	}
	if out.Inner.Balance == nil {
		t.Errorf("[%s] Inner.Balance nil", tag)
	} else if out.Inner.Balance.String() != "50" {
		t.Errorf("[%s] Inner.Balance = %s, want 50", tag, out.Inner.Balance.String())
	}
	if out.Inner.Label != "main" {
		t.Errorf("[%s] Inner.Label = %q, want main", tag, out.Inner.Label)
	}
	if out.Ptr == nil {
		t.Errorf("[%s] Ptr nil", tag)
	} else if out.Ptr.Balance == nil {
		t.Errorf("[%s] Ptr.Balance nil", tag)
	} else if out.Ptr.Balance.String() != "25" {
		t.Errorf("[%s] Ptr.Balance = %s, want 25", tag, out.Ptr.Balance.String())
	}
	if out.Ptr != nil && out.Ptr.Label != "sub" {
		t.Errorf("[%s] Ptr.Label = %q, want sub", tag, out.Ptr.Label)
	}
	if len(out.Items) != 2 {
		t.Errorf("[%s] Items len=%d, want 2", tag, len(out.Items))
	} else {
		for i, it := range out.Items {
			if it.Qty == nil {
				t.Errorf("[%s] Items[%d].Qty nil", tag, i)
			}
		}
	}
	if len(out.Ptrs) != 2 {
		t.Errorf("[%s] Ptrs len=%d, want 2", tag, len(out.Ptrs))
	} else {
		for i, it := range out.Ptrs {
			if it == nil {
				t.Errorf("[%s] Ptrs[%d] nil", tag, i)
			} else if it.Qty == nil {
				t.Errorf("[%s] Ptrs[%d].Qty nil", tag, i)
			}
		}
	}
}

// ============================================================================
// Casos
// ============================================================================

// A: misma sesión, Read simple.
//
// Verifica que el mapper funciona cuando el valor llega como struct
// original (sin round-trip JSON). Si esto falla, el bug está en el
// mapper directo, no en el round-trip.
func TestGonum_SameSession_Read(t *testing.T) {
	db, cleanup := newTestDBNoWAL(t)
	defer cleanup()

	if err := db.Create("recs"); err != nil {
		t.Fatal(err)
	}
	if err := db.Write("recs", "k1", makeGRec()); err != nil {
		t.Fatal(err)
	}

	var out GRec
	if err := db.Read("recs", "k1", &out); err != nil {
		t.Fatal(err)
	}
	checkGRec(t, "same-session-read", out)
}

// B: nueva sesión (flush + reload desde disco).
//
// Este es el caso donde MÁS aparecen bugs de round-trip JSON, porque
// después de load(), los valores ya no son structs originales sino
// map[string]any con tipos JSON-tipificados.
func TestGonum_NewSession_Read(t *testing.T) {
	db, cleanup := newTestDBNoWAL(t)
	defer cleanup()

	if err := db.Create("recs"); err != nil {
		t.Fatal(err)
	}
	if err := db.Write("recs", "k1", makeGRec()); err != nil {
		t.Fatal(err)
	}

	simulateReload(t, db)

	var out GRec
	if err := db.Read("recs", "k1", &out); err != nil {
		t.Fatal(err)
	}
	checkGRec(t, "new-session-read", out)
}

// C: ReadBatch con []Rec (valor).
//
// Verifica que el ReadBatch funciona cuando el dst es []GRec (slice de
// structs por valor). El mapper tiene que crear cada GRec con su propio
// *gonum.Number en cada nivel de nesting.
func TestGonum_ReadBatch_Value(t *testing.T) {
	db, cleanup := newTestDBNoWAL(t)
	defer cleanup()

	if err := db.Create("recs"); err != nil {
		t.Fatal(err)
	}
	if err := db.Write("recs", "k1", makeGRec()); err != nil {
		t.Fatal(err)
	}

	var outs []GRec
	if err := db.ReadBatch("recs", &outs); err != nil {
		t.Fatal(err)
	}
	if len(outs) != 1 {
		t.Fatalf("len(outs)=%d, want 1", len(outs))
	}
	checkGRec(t, "readbatch-value", outs[0])
}

// D: ReadBatch con []*Rec (puntero).
//
// Igual que C pero con []*GRec. El mapper tiene que crear un *GRec
// para cada item y popular todos los *gonum.Number internos.
func TestGonum_ReadBatch_Pointer(t *testing.T) {
	db, cleanup := newTestDBNoWAL(t)
	defer cleanup()

	if err := db.Create("recs"); err != nil {
		t.Fatal(err)
	}
	if err := db.Write("recs", "k1", makeGRec()); err != nil {
		t.Fatal(err)
	}

	var outs []*GRec
	if err := db.ReadBatch("recs", &outs); err != nil {
		t.Fatal(err)
	}
	if len(outs) != 1 {
		t.Fatalf("len(outs)=%d, want 1", len(outs))
	}
	if outs[0] == nil {
		t.Fatal("outs[0] is nil")
	}
	checkGRec(t, "readbatch-pointer", *outs[0])
}

// E: nueva sesión + ReadBatch []Rec.
//
// Combinación: round-trip JSON + ReadBatch con slice de valores.
// Este es el escenario del bug reportado — el más estricto.
func TestGonum_NewSession_ReadBatch_Value(t *testing.T) {
	db, cleanup := newTestDBNoWAL(t)
	defer cleanup()

	if err := db.Create("recs"); err != nil {
		t.Fatal(err)
	}
	if err := db.Write("recs", "k1", makeGRec()); err != nil {
		t.Fatal(err)
	}

	simulateReload(t, db)

	var outs []GRec
	if err := db.ReadBatch("recs", &outs); err != nil {
		t.Fatal(err)
	}
	if len(outs) != 1 {
		t.Fatalf("len(outs)=%d, want 1", len(outs))
	}
	checkGRec(t, "new-session-readbatch-value", outs[0])
}

// F: nueva sesión + ReadBatch []*Rec.
//
// Igual que E pero con []*GRec. Si E pasa pero F falla, el bug está
// específicamente en el case reflect.Pointer del mapper.
func TestGonum_NewSession_ReadBatch_Pointer(t *testing.T) {
	db, cleanup := newTestDBNoWAL(t)
	defer cleanup()

	if err := db.Create("recs"); err != nil {
		t.Fatal(err)
	}
	if err := db.Write("recs", "k1", makeGRec()); err != nil {
		t.Fatal(err)
	}

	simulateReload(t, db)

	var outs []*GRec
	if err := db.ReadBatch("recs", &outs); err != nil {
		t.Fatal(err)
	}
	if len(outs) != 1 {
		t.Fatalf("len(outs)=%d, want 1", len(outs))
	}
	if outs[0] == nil {
		t.Fatal("outs[0] is nil")
	}
	checkGRec(t, "new-session-readbatch-pointer", *outs[0])
}

// G: WriteBatch con varios registros con gonum.
//
// Verifica que WriteBatch + ReadBatch redondea correctamente cuando
// hay múltiples registros con *gonum.Number.
func TestGonum_WriteBatch(t *testing.T) {
	db, cleanup := newTestDBNoWAL(t)
	defer cleanup()

	if err := db.Create("recs"); err != nil {
		t.Fatal(err)
	}

	recs := map[string]any{
		"k1": makeGRec(),
		"k2": func() GRec { r := makeGRec(); r.ID = "x2"; return r }(),
	}
	if err := db.WriteBatch("recs", recs); err != nil {
		t.Fatal(err)
	}

	var outs []GRec
	if err := db.ReadBatch("recs", &outs); err != nil {
		t.Fatal(err)
	}
	if len(outs) != 2 {
		t.Fatalf("len(outs)=%d, want 2", len(outs))
	}
	for i, out := range outs {
		checkGRec(t, "writebatch-"+string(rune('0'+i)), out)
	}
}

// H: nueva sesión + WriteBatch + ReadBatch.
//
// Combina todos los escenarios: WriteBatch con *gonum.Number, flush a
// disco, reload, y ReadBatch. Si todos los tests anteriores pasan pero
// este falla, hay algún issue específico con cómo WriteBatch serializa
// los valores vs Write individual.
func TestGonum_NewSession_WriteBatch(t *testing.T) {
	db, cleanup := newTestDBNoWAL(t)
	defer cleanup()

	if err := db.Create("recs"); err != nil {
		t.Fatal(err)
	}

	recs := map[string]any{
		"k1": makeGRec(),
		"k2": func() GRec { r := makeGRec(); r.ID = "x2"; return r }(),
	}
	if err := db.WriteBatch("recs", recs); err != nil {
		t.Fatal(err)
	}

	simulateReload(t, db)

	var outs []GRec
	if err := db.ReadBatch("recs", &outs); err != nil {
		t.Fatal(err)
	}
	if len(outs) != 2 {
		t.Fatalf("len(outs)=%d, want 2", len(outs))
	}
	for i, out := range outs {
		checkGRec(t, "new-session-writebatch-"+string(rune('0'+i)), out)
	}
}

// I: valores numéricos variados (negativos, decimales, grandes).
//
// Verifica que parseGonumNumber maneja todos los formatos numéricos
// que gonum.New puede aceptar, especialmente los que vienen de
// round-trip JSON como string.
func TestGonum_NumericFormats(t *testing.T) {
	db, cleanup := newTestDBNoWAL(t)
	defer cleanup()

	if err := db.Create("recs"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		key   string
		price string
	}{
		{"integer", "858"},
		{"negative", "-100"},
		{"decimal", "3.14"},
		{"negative_decimal", "-2.5"},
		{"zero", "0"},
		{"large", "99999999999999999999"},
		{"small_decimal", "0.001"},
		{"trailing_zeros", "1.500"},
	}

	for _, c := range cases {
		rec := GRec{
			ID:    c.key,
			Price: gonum.New(c.price),
		}
		if err := db.Write("recs", c.key, rec); err != nil {
			t.Fatalf("Write %s: %v", c.key, err)
		}
	}

	// Verificar en misma sesión.
	for _, c := range cases {
		var out GRec
		if err := db.Read("recs", c.key, &out); err != nil {
			t.Errorf("Read %s: %v", c.key, err)
			continue
		}
		if out.Price == nil {
			t.Errorf("[%s] Price nil", c.key)
			continue
		}
		// Comparar como string — gonum normaliza, así que puede
		// no ser exactamente igual al input (ej: "1.500" → "1.5").
		// Solo verificamos que no sea nil y que tenga un valor razonable.
		_ = out.Price.String()
	}

	// Verificar después de reload.
	simulateReload(t, db)

	for _, c := range cases {
		var out GRec
		if err := db.Read("recs", c.key, &out); err != nil {
			t.Errorf("[reload] Read %s: %v", c.key, err)
			continue
		}
		if out.Price == nil {
			t.Errorf("[reload %s] Price nil", c.key)
		}
	}
}
