// mapper_test.go
package mapper

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/orislabsdev/gonum"
)

// ---- Structs de prueba ----

type SimpleGonum struct {
	Name   string        `json:"name"`
	Amount *gonum.Number `json:"amount"`
}

type NestedGonum struct {
	ID    string        `json:"id"`
	Price *gonum.Number `json:"price"`
	Inner InnerGonum    `json:"inner"`
	Items []ItemGonum   `json:"items"`
	Ptr   *InnerGonum   `json:"ptr"`
}

type InnerGonum struct {
	Balance *gonum.Number `json:"balance"`
	Label   string        `json:"label"`
}

type ItemGonum struct {
	Code string        `json:"code"`
	Qty  *gonum.Number `json:"qty"`
}

type PtrItemGonum struct {
	Code string        `json:"code"`
	Qty  *gonum.Number `json:"qty"`
}

type WithPtrSlice struct {
	Items []*PtrItemGonum `json:"items"`
}

// ---- Tests ----

// Test 1: gonum como string directo (el caso que reportaste)
func TestGonumFromString(t *testing.T) {
	m := map[string]any{
		"name":   "alice",
		"amount": "858",
	}
	var out SimpleGonum
	if err := MapToStruct(m, &out); err != nil {
		t.Fatalf("MapToStruct error: %v", err)
	}
	if out.Name != "alice" {
		t.Errorf("Name = %q, want alice", out.Name)
	}
	if out.Amount == nil {
		t.Fatal("Amount is nil — parseGonumNumber falló con string")
	}
	t.Logf("Amount = %v", out.Amount)
}

// Test 2: gonum ya como *gonum.Number (sin serialización)
func TestGonumFromPointer(t *testing.T) {
	n := gonum.New("858")
	m := map[string]any{
		"name":   "alice",
		"amount": n,
	}
	var out SimpleGonum
	if err := MapToStruct(m, &out); err != nil {
		t.Fatalf("MapToStruct error: %v", err)
	}
	if out.Amount == nil {
		t.Fatal("Amount is nil — pointer directo no se asignó")
	}
	t.Logf("Amount = %v", out.Amount)
}

// Test 3: round-trip JSON completo (simula flush → load → Read)
func TestGonumJSONRoundtrip(t *testing.T) {
	original := SimpleGonum{
		Name:   "alice",
		Amount: gonum.New("858"),
	}

	// Simular flush a disco
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("JSON en disco: %s", b)

	// Simular load: unmarshal a map[string]any
	var asMap map[string]any
	if err := json.Unmarshal(b, &asMap); err != nil {
		t.Fatalf("unmarshal a map: %v", err)
	}
	t.Logf("Amount tras JSON: %#v (type %T)", asMap["amount"], asMap["amount"])

	// Simular Read → MapToStruct
	var out SimpleGonum
	if err := MapToStruct(asMap, &out); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}
	if out.Amount == nil {
		t.Fatalf("Amount is nil tras roundtrip JSON. Valor raw era %T = %#v",
			asMap["amount"], asMap["amount"])
	}
	t.Logf("Amount = %v", out.Amount)
}

// Test 4: struct anidado con gonum (el caso que probablemente falla)
func TestGonumNested(t *testing.T) {
	m := map[string]any{
		"id":    "x1",
		"price": "100",
		"inner": map[string]any{
			"balance": "50",
			"label":   "main",
		},
		"items": []any{
			map[string]any{"code": "a", "qty": "1"},
			map[string]any{"code": "b", "qty": "2"},
		},
		"ptr": map[string]any{
			"balance": "25",
			"label":   "sub",
		},
	}

	var out NestedGonum
	if err := MapToStruct(m, &out); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}

	if out.Price == nil {
		t.Error("Price is nil")
	} else {
		t.Logf("Price = %v", out.Price)
	}

	if out.Inner.Balance == nil {
		t.Error("Inner.Balance is nil")
	} else {
		t.Logf("Inner.Balance = %v", out.Inner.Balance)
	}

	if out.Ptr == nil {
		t.Fatal("Ptr is nil")
	}
	if out.Ptr.Balance == nil {
		t.Error("Ptr.Balance is nil")
	} else {
		t.Logf("Ptr.Balance = %v", out.Ptr.Balance)
	}

	if len(out.Items) != 2 {
		t.Fatalf("Items len = %d, want 2", len(out.Items))
	}
	for i, it := range out.Items {
		if it.Qty == nil {
			t.Errorf("Items[%d].Qty is nil (code=%q)", i, it.Code)
		} else {
			t.Logf("Items[%d].Qty = %v", i, it.Qty)
		}
	}
}

// Test 5: []any con *gonum.Number ya nativo dentro de slice
func TestGonumInSliceNative(t *testing.T) {
	m := map[string]any{
		"items": []any{
			map[string]any{"code": "a", "qty": gonum.New("1")},
			map[string]any{"code": "b", "qty": gonum.New("2")},
		},
	}
	var out WithPtrSlice
	if err := MapToStruct(m, &out); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}
	for i, it := range out.Items {
		if it == nil {
			t.Errorf("Items[%d] is nil", i)
			continue
		}
		if it.Qty == nil {
			t.Errorf("Items[%d].Qty is nil", i)
		}
	}
}

// Test 6: JSON con UseNumber (json.Number) — caso típico de algunos decoders
func TestGonumFromJSONNumber(t *testing.T) {
	jsonStr := `{"amount": 858}`
	dec := json.NewDecoder(strings.NewReader(jsonStr))
	dec.UseNumber()

	var result map[string]any
	if err := dec.Decode(&result); err != nil {
		t.Fatal(err)
	}

	raw := result["amount"]
	t.Logf("raw type=%T value=%#v", raw, raw)

	m := map[string]any{
		"name":   "alice",
		"amount": json.Number("858"),
	}
	var out SimpleGonum
	if err := MapToStruct(m, &out); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}
	if out.Amount == nil {
		t.Fatal("Amount is nil — json.Number no manejado")
	}
	t.Logf("Amount = %v", out.Amount)
}

// Test 7: round-trip completo de struct anidado por JSON
func TestGonumNestedJSONRoundtrip(t *testing.T) {
	original := NestedGonum{
		ID:    "x1",
		Price: gonum.New("100"),
		Inner: InnerGonum{
			Balance: gonum.New("50"),
			Label:   "main",
		},
		Items: []ItemGonum{
			{Code: "a", Qty: gonum.New("1")},
			{Code: "b", Qty: gonum.New("2")},
		},
		Ptr: &InnerGonum{
			Balance: gonum.New("25"),
			Label:   "sub",
		},
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("JSON: %s", b)

	var asMap map[string]any
	if err := json.Unmarshal(b, &asMap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Log de tipos concretos para diagnóstico
	t.Logf("price type: %T = %#v", asMap["price"], asMap["price"])
	if inner, ok := asMap["inner"].(map[string]any); ok {
		t.Logf("inner.balance type: %T = %#v", inner["balance"], inner["balance"])
	}
	if items, ok := asMap["items"].([]any); ok && len(items) > 0 {
		if it0, ok := items[0].(map[string]any); ok {
			t.Logf("items[0].qty type: %T = %#v", it0["qty"], it0["qty"])
		}
	}

	var out NestedGonum
	if err := MapToStruct(asMap, &out); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}

	if out.Price == nil {
		t.Error("Price is nil tras roundtrip")
	}
	if out.Inner.Balance == nil {
		t.Error("Inner.Balance is nil tras roundtrip")
	}
	if out.Ptr == nil || out.Ptr.Balance == nil {
		t.Error("Ptr.Balance is nil tras roundtrip")
	}
	for i, it := range out.Items {
		if it.Qty == nil {
			t.Errorf("Items[%d].Qty is nil tras roundtrip", i)
		}
	}
}

// Test 8: campo gonum ausente en el map (debe quedar nil sin error)
func TestGonumMissing(t *testing.T) {
	m := map[string]any{"name": "alice"}
	var out SimpleGonum
	if err := MapToStruct(m, &out); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}
	if out.Amount != nil {
		t.Errorf("Amount = %v, want nil (ausente)", out.Amount)
	}
}

// Test 9: campo gonum como null en el JSON
func TestGonumNull(t *testing.T) {
	m := map[string]any{"name": "alice", "amount": nil}
	var out SimpleGonum
	if err := MapToStruct(m, &out); err != nil {
		t.Fatalf("MapToStruct: %v", err)
	}
	if out.Amount != nil {
		t.Errorf("Amount = %v, want nil (null)", out.Amount)
	}
}
