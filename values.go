package gocraft

import abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"

// Value is one positional value of a plugin-defined event.
//
// An alias rather than a type of its own: it is the same vocabulary the wire
// carries, and a parallel type would mean converting every field of every
// emission for no gain. The alias is what keeps a plugin author from importing
// the contract directly — the SDK is the only package they should have to name.
type Value = abi.Value

// The constructors, re-exported for the same reason.
func Bool(value bool) Value      { return abi.Bool(value) }
func Int64(value int64) Value    { return abi.Int64(value) }
func Double(value float64) Value { return abi.Double(value) }
func String(value string) Value  { return abi.String(value) }
func Bytes(value []byte) Value   { return abi.Bytes(value) }

// List nests values, which is how a field declared []Tier or a record with
// fields of its own reaches the wire.
func List(values ...Value) Value { return abi.List(values...) }

// ValueKind says which of a Value's fields is the meaningful one.
type ValueKind = abi.ValueKind

// The kinds, re-exported for the same reason as the constructors: reading a
// value means comparing against one, and an author asked to import the contract
// for that would be importing it for everything eventually.
const (
	ValueInvalid = abi.ValueInvalid
	ValueBool    = abi.ValueBool
	ValueInt64   = abi.ValueInt64
	ValueDouble  = abi.ValueDouble
	ValueString  = abi.ValueString
	ValueBytes   = abi.ValueBytes
	ValueList    = abi.ValueList
)
