# 📦 PolarysDB – Changelog

## v1.0.2 (Stable)

**Release Date:** *27, October 2025*

**Repository:** [polarysfoundation/polarysdb](https://github.com/polarysfoundation/polarysdb)

---

### 🚀 Features

* Added **database export utility** to produce human-readable (non-encrypted) dumps, simplifying audits and data migration.
* Introduced **read-only mode** for database opening, enabling safe data inspection and analytics without modification risks.

---

### 🧰 Improvements

* Refactored database initialization logic for better clarity, safety, and lifecycle management.
* Enhanced error handling for encrypted import/export (`ExportEncrypted`, `ImportEncrypted`) to provide more descriptive error messages and prevent silent failures.
* Improved key rotation (`ChangeKey`) mechanism with validation of the previous key and consistency checks across re-encrypted records.

---

### 🐞 Bug Fixes

* Fixed an issue in `ReadBatch` where some records could be skipped under specific concurrent access conditions.
* Resolved a `Write` bug causing accidental overwrites when writing to a non-existent table.
* Added strict validation to `BytesToKey` conversion to prevent data truncation with oversized inputs.

---

### ⚙️ Migration Notes

* **Backward compatible** with all 1.0.x versions.
* If using `ChangeKey`, **ensure a full backup** before rotating keys, as the process re-encrypts all stored data.
* Users relying on concurrent `ReadBatch` operations are advised to review previous query integrity after upgrading.

---

### 📚 Documentation & Tests

* Updated `README.md` with examples for read-only mode and encrypted import/export usage.
* Extended unit test coverage for encryption, export/import, and key-rotation workflows.

---

### 🔖 Summary

PolarysDB v1.0.2 focuses on **stability, security, and auditability**.
It strengthens cryptographic workflows, introduces safer access modes, and improves data reliability for production environments.

---
