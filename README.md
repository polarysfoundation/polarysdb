# Polarys DB

Polarys DB is a Go-based database module that provides common types and utility functions for handling cryptographic keys and a simple in-memory database with encryption support.

## Overview

This project includes a set of utility functions to convert between different representations of cryptographic keys and a simple in-memory database that supports encryption. The main type used is `Key`, which is a fixed-size array of bytes.

## Functions

### BytesToKey

```go
func BytesToKey(b []byte) Key
```

Converts a byte slice to a `Key`. If the byte slice is longer than the key length, it takes the last `keyLen` bytes.

### StringToKey

```go
func StringToKey(s string) Key
```

Converts a string to a `Key` by converting the string to a byte slice and then calling `BytesToKey`.

### KeyToByte

```go
func (k Key) KeyToByte() []byte
```

Converts a `Key` to a byte slice.

### SetBytes

```go
func (a *Key) SetBytes(b []byte)
```

Sets the bytes of a `Key` from a byte slice. If the byte slice is longer than the key length, it takes the last `keyLen` bytes.

## Database

The `Database` struct represents a simple in-memory database with encryption support. It provides methods to create, read, update, and delete records.

### Init

```go
func Init(keyDb common.Key, dirPath string) (*Database, error)
```

Initializes a new `Database` instance with the provided encryption key and directory path.

### Exist

```go
func (db *Database) Exist(table string) bool
```

Checks if a table exists in the database.

### Create

```go
func (db *Database) Create(table, key string, value interface{}) error
```

Creates a new record in the specified table.

### Write

```go
func (db *Database) Write(table, key string, value interface{}) error
```

Updates an existing record in the specified table.

### Delete

```go
func (db *Database) Delete(table, key string) error
```

Deletes a record from the specified table.

### Read

```go
func (db *Database) Read(table, key string) (interface{}, bool)
```

Reads a record from the specified table.

### ReadBatch

```go
func (db *Database) ReadBatch(table string) []interface{}
```

Reads all records from the specified table.

### Close

```go
func (db *Database) Close() error
```

Closes the database.

### Export && ExportEncrypted

```go
func (db *Database) Export(key common.Key, path string) error
func (db *Database) ExportEncrypted(key common.Key, path string) error
```

Export && ExportEncrypted

### Import && ImportEncrypted

```go
func (db *Database) Import(key common.Key, path string) error
func (db *Database) ImportEncrypted(key common.Key, path string) error
```

Import && ImportEncrypted

### ChangeKey

```go
func (db *Database) ChangeKey(oldKey, newKey common.Key) error
```

ChangeKey update database key.

## Security Key

The security key is a 32-byte key used to encrypt and decrypt information stored in the database.

### GenerateKey

```go
func GenerateKey() Key{}
```

generate a new 32-bytes key

### GenerateKeyFromBytes

```go
func GenerateKeyFromBytes(b []byte) Key{}
```

generate a new 32-bytes key from bytes

## Usage

To use the database, import the `polarysdb` package. Below is an example demonstrating how to initialize the database, create a table, write a record, and read it back.

```go
package main

import (
	"fmt"

	"github.com/polarysfoundation/polarysdb"
)

func main() {
	// Generate a new encryption key for the database.
	key := polarysdb.GenerateKey()
	fmt.Println("Generated a new database key.")

	// Initialize the database. This will create the db file if it doesn't exist.
	db, err := polarysdb.Init(key, "./data")
	if err != nil {
		fmt.Println("Error initializing database:", err)
		return
	}
	defer db.Close() // Ensure the file watcher is stopped when main exits.

	// 1. Create a new table called "users".
	if err := db.Create("users"); err != nil {
		fmt.Println("Error creating table:", err)
		return
	}

	// 2. Write a record to the "users" table.
	userData := map[string]any{"name": "John Doe", "email": "john.doe@example.com"}
	if err := db.Write("users", "user1", userData); err != nil {
		fmt.Println("Error writing record:", err)
		return
	}
	fmt.Println("Successfully wrote record for user1.")

	// 3. Read the record back from the database.
	user, exists := db.Read("users", "user1")
	if !exists {
		fmt.Println("Failed to read record for user1.")
		return
	}
}
```

This example converts a string to a `Key`, initializes the database, creates a new record, and reads the record.

## Installation

To install the package, use the following command:

```sh
go get github.com/polarysfoundation/polarysdb
```

## License

This project is licensed under the [MIT LICENSE](LICENSE).
