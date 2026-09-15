Go++ Feature Spec: Class Introspection

Goal

Provide lightweight compile-time-generated runtime metadata for every Go++ class so generic library code can inspect the concrete class name and fields.

This is a language primitive, not an ORM feature.

Primary use cases:

- generic persistence
- serialization
- validation
- debugging
- form generation
- generic utilities

Core syntax

Instance introspection:

obj.class

Inside methods:

this.class

Static type introspection:

Employee.class

Generic type introspection, when T is known to be a Go++ class:

T.class

Examples:

class Employee {
    Id *string
    Name string
    Salary int
}

e := Employee()

println(e.class.name)
println(Employee.class.name)

for f := range e.class.fields {
    println(f.name)
}

Required semantics

1. Every Go++ class has an associated class descriptor.

2. `.class` returns that descriptor.

3. For an object instance, `.class` must return the descriptor of the most-derived runtime class.

Example:

class Model {
    func Debug() {
        println(this.class.name)
    }
}

class Employee : Model {}

Employee().Debug()

prints:

Employee

It must NOT print Model.

4. Static access is also allowed:

Employee.class

This refers directly to Employee's class descriptor.

5. Native Go structs/types do not automatically gain Go++ class metadata.

Example:

http.Request.class

should be invalid unless a separate reflection mechanism is explicitly added later.

Class descriptor

Conceptual built-in type:

Class

Minimum required members:

class.name
class.fields

Example:

let c = Employee.class

c.name
c.fields

`name`

`class.name` returns the Go++ class name as a string.

Example:

Employee.class.name == "Employee"

For:

package foo

class Employee {}

the initial implementation should return:

"Employee"

A fully-qualified name may be exposed separately later if needed.

Fields

`class.fields` returns metadata for all effective instance fields of the class.

Example:

class Model {
    Id *string
}

class Employee : Model {
    Name string
    Salary int
}

Employee.class.fields

must include:

Id
Name
Salary

Inherited fields are included.

Field metadata

Conceptual built-in type:

Field

Minimum required members:

field.name
field.type
field.get(obj)
field.set(obj, value)
field.addr(obj)

Recommended initial surface:

field.name        string
field.type        Type
field.get(obj)    any
field.set(obj,v)
field.addr(obj)   any

Exact compiler/runtime representation is implementation-defined.

`field.name`

Returns the source field name.

Example:

Employee.class.fields[0].name

`field.type`

Returns type metadata describing the declared field type.

Initial implementation may expose this as a lightweight built-in Type descriptor.

Required minimum capability:

field.type.name

Examples:

string
int
*string
time.Time

Do not require a complete reflection system in v1.

`field.get(obj)`

Returns the current value of that field from obj.

Example:

for f := range e.class.fields {
    println(f.name, f.get(e))
}

For:

e.Name = "Bob"

the Name field returns "Bob".

The compiler/runtime must correctly resolve inherited fields against the most-derived object.

`field.set(obj, value)`

Assigns a value to the corresponding field.

Example:

f.set(e, "Bob")

Type checking should occur whenever the compiler knows the field/value types.

For truly dynamic field iteration, runtime type validation may be necessary.

`field.addr(obj)`

Returns a pointer/reference suitable for APIs that require field addresses.

Primary target:

database/sql Scan

Example:

dest := []any{}

for f := range Employee.class.fields {
    dest = append(dest, f.addr(e))
}

err := row.Scan(dest...)

For Employee:

class Employee {
    Id *string
    Name string
    Salary int
}

the resulting behavior must be equivalent to:

row.Scan(
    &e.Id,
    &e.Name,
    &e.Salary,
)

This is an important interoperability requirement.

Runtime concrete type

The object model must retain the most-derived object identity.

Example:

class Model {
    func Fields() {
        for f := range this.class.fields {
            println(f.name)
        }
    }
}

class Employee : Model {
    Name string
    Salary int
}

Employee().Fields()

must enumerate Employee's effective fields, not merely Model's fields.

Inheritance

Fields from base classes participate in introspection.

Example:

class A {
    AField int
}

class B {
    BField string
}

class C : A, B {
    CField bool
}

C.class.fields

must expose all effective fields.

Multiple inheritance conflicts

If inherited field names clash and Go++ requires qualification for normal access, introspection must retain enough ownership information to distinguish them.

Recommended additional metadata:

field.owner

Example:

class A { Name string }
class B { Name string }
class C : A, B {}

C.class.fields contains two Name fields:

Name, owner=A
Name, owner=B

Do not silently collapse physically distinct inherited fields.

For v1, Field should therefore preferably include:

field.name
field.owner
field.type
field.get(...)
field.set(...)
field.addr(...)

`field.owner`

Returns the class that physically declares the field.

This can also help libraries deal with inherited clashes.

Field order

Define deterministic field ordering.

Recommended:

1. base classes in declared inheritance order
2. recursively include their fields
3. class's own fields last
4. preserve declaration order within each declaring class

Example:

class C : A, B {
    X int
    Y int
}

fields appear as:

A fields
B fields
X
Y

This ordering should be stable across builds.

Generated implementation

The compiler should generate metadata for every Go++ class.

Conceptually:

type __GppField struct {
    Name  string
    Owner *__GppClass
    Type  *__GppType
    Get   func(unsafe.Pointer) any
    Set   func(unsafe.Pointer, any)
    Addr  func(unsafe.Pointer) any
}

type __GppClass struct {
    Name   string
    Fields []__GppField
}

For:

class Employee : Model {
    Name string
    Salary int
}

compiler output may conceptually include:

var __EmployeeClass = __GppClass{
    Name: "Employee",
    Fields: []__GppField{
        ...Model fields...,
        {
            Name: "Name",
            Get: func(root unsafe.Pointer) any {
                return (*Employee)(root).Name
            },
            Addr: func(root unsafe.Pointer) any {
                return &(*Employee)(root).Name
            },
        },
        {
            Name: "Salary",
            Get: func(root unsafe.Pointer) any {
                return (*Employee)(root).Salary
            },
            Addr: func(root unsafe.Pointer) any {
                return &(*Employee)(root).Salary
            },
        },
    },
}

Exact representation may differ.

Object metadata

Every Go++ class instance already needs hidden runtime metadata for polymorphism.

Extend that metadata so the runtime object can reach its most-derived Class descriptor.

Conceptually:

object header:
    root
    vtable
    class

Therefore:

this.class

is constant-time metadata access.

Do not use Go reflection to rediscover class structure on every call.

Compiler-generated static metadata is preferred.

Native Go interoperability

Do not change native Go types.

Go++ class metadata exists only for Go++ classes.

Generated Go fields remain ordinary Go fields.

`field.addr(obj)` must return actual Go pointers usable with APIs such as:

database/sql
encoding
other Go libraries accepting pointers/interfaces

Visibility

All fields may initially appear in `.fields`, including lowercase/private Go++ fields.

Visibility affects source-code access, not necessarily metadata access.

However, if stronger encapsulation is desired later, an additional filter or metadata flag may be introduced:

field.exported
field.visible

Do not complicate v1 unless necessary.

Class descriptor identity

Employee.class should return the same descriptor identity for all Employee instances.

Example:

a := Employee()
b := Employee()

a.class == b.class

should be true.

Derived instance:

class Manager : Employee {}

m := Manager()

m.class == Manager.class

and:

m.class != Employee.class

Built-in descriptor types

`Class`, `Field`, and optionally `Type` are compiler/runtime built-ins.

They do not need to be ordinary user-defined Go++ classes.

Minimum v1 API:

Class:
    name string
    fields []Field

Field:
    name string
    owner Class
    type Type
    get(object) any
    set(object, any)
    addr(object) any

Type:
    name string

Additional metadata can be added later without changing the basic model.

Example: generic CRUD library

This feature should make ordinary Go++ code like this possible:

class Model {
    func Create(db *sql.DB) error {
        c := this.class

        columns := []string{}
        values := []any{}

        for f := range c.fields {
            columns = append(columns, f.name)
            values = append(values, f.get(this))
        }

        // build ordinary SQL
        // db.Exec(query, values...)

        return nil
    }
}

No database behavior is compiler built-in.

Example: database/sql Scan

func scanInto[T](row *sql.Row, obj T) error {
    dest := []any{}

    for f := range obj.class.fields {
        dest = append(dest, f.addr(obj))
    }

    return row.Scan(dest...)
}

This must lower to ordinary database/sql usage.

Errors

Invalid:

x.class

when x is not a Go++ class instance.

Invalid:

http.Request.class

for native Go types in v1.

Invalid:

field.addr(obj)

when obj is not compatible with the class/owner expected by the field.

Diagnostics should identify the mismatched class/type.

Suggested implementation order

1. Add Class descriptor representation.
2. Add Field descriptor representation.
3. Generate one Class descriptor per Go++ class.
4. Add hidden class pointer to runtime object metadata.
5. Implement instance `.class`.
6. Implement `Class.name`.
7. Implement `Class.fields`.
8. Generate Field.get accessors.
9. Generate Field.addr accessors.
10. Generate Field.set accessors.
11. Support inherited fields.
12. Support multiple-inheritance ownership metadata.
13. Implement static `Employee.class`.
14. Implement generic `T.class` only after normal static class access works.

Tests

Basic class name:

class Employee {}

assert(Employee.class.name == "Employee")

Runtime derived name:

class Model {
    func Name() string {
        return this.class.name
    }
}

class Employee : Model {}

assert(Employee().Name() == "Employee")

Fields:

class Employee {
    Name string
    Age int
}

assert(Employee.class.fields contains Name and Age)

Inherited fields:

class Model {
    Id *string
}

class Employee : Model {
    Name string
}

Employee.class.fields must expose Id and Name.

Get:

e := Employee()
e.Name = "Bob"

find field "Name"
assert(field.get(e) == "Bob")

Set:

field.set(e, "Alice")
assert(e.Name == "Alice")

Address:

row.Scan(field.addr(e))

must behave like:

row.Scan(&e.Name)

Multiple inheritance:

class A { X int }
class B { X int }
class C : A, B {}

metadata must preserve both fields and their owners.

Design principle

Go++ classes should know what they are.

`this.class` exposes the most-derived runtime class.

`Class.fields` exposes compiler-generated structural metadata.

This metadata should be sufficient for ordinary Go++ libraries to implement generic persistence and other repetitive structural operations without baking those features into the language itself.
