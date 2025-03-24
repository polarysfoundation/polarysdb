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
func Init(keyDb string, dirPath string) (*Database, error)
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

To use these functions and the database, import the `common` and `polarysdb` packages and call the desired function with the appropriate parameters. Below is an example:

```go
package main

import (
    "fmt"
    "github.com/polarysfoundation/polarys_db"
)

func main() {
    key := polarysdb.GenerateKey()
    fmt.Printf("key: %s", key.KeyToString())

    db, err := polarysdb.Init(key.KeyToString(), "./data")
    if err != nil {
        fmt.Println("Error initializing database:", err)
        return
    }

    err = db.Create("users", "user1", map[string]interface{}{"name": "John Doe"})
    if err != nil {
        fmt.Println("Error creating record:", err)
        return
    }

    user, exists := db.Read("users", "user1")
    if exists {
        fmt.Println("User:", user)
    }
}
```

This example converts a string to a `Key`, initializes the database, creates a new record, and reads the record.

## Installation

To install the package, use the following command:

```sh
go get github.com/polarysfoundation/polarys_db
```

## License

This project is licensed under the MIT License.
