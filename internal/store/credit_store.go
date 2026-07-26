package store

// CreditStore is a placeholder for the credit persistence layer that Task 8
// implements. It exists now, empty, only so api.NewRouter's signature
// (which takes *CreditStore per the Task 5 design) compiles; Task 5 always
// passes nil for it, and Task 9 wires the credit routes once this type
// gains real fields and methods.
type CreditStore struct{}
