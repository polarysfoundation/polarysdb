# 🚀 PolarysDB v1.1.3 Release Notes

This is a patch update for PolarysDB, focusing on project security and dependency maintenance.

## 📝 Change Summary

### 🛡️ Security
- **Security Policy**: Added a formal `SECURITY.md` file to the repository. This document outlines the process for reporting vulnerabilities and our commitment to security.
- **Vulnerability Reporting**: Linked the security policy in the `README.md` to ensure it is easily accessible to researchers and contributors.

### 📦 Dependencies
- **Encryption Library**: Updated `github.com/polarysfoundation/pm-256` from a pseudo-version to the stable `v1.1.0`. This ensures better reliability and follows semantic versioning practices.

## 🚀 How to Update

To update to the latest version, run:

```bash
go get github.com/polarysfoundation/polarysdb@v1.1.3
```

---
*For a full list of changes, please see the [CHANGELOG.md](CHANGELOG.md).*
