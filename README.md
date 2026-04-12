# Importing pan

```go
    import "impractical.co/pan"
```

# About pan

`pan` is an SQL query building and response unmarshalling library for Go.
It is designed to be compatible with MySQL, PostgreSQL, and SQLite, but should be more or less agnostic.
Please let us know if your favourite SQL flavour is not supported.

`pan` is not designed to be an ORM, but it still eliminates much of the boilerplate code around writing queries and scanning over rows.

`pan`’s design focuses on reducing repetition and hardcoded strings in your queries, but without limiting your ability to write any form of query you want.
It is meant to be the smallest possible abstraction on top of SQL.

Docs can be found on [pkg.go.dev](https://pkg.go.dev/impractical.co/pan).

# Using pan

`pan` revolves around structs that fill the [`SQLTableNamer`](https://pkg.go.dev/impractical.co/pan#SQLTableNamer) interface, by implementing the `GetSQLTableName` method, which just returns the name of the table that should be mapped to that struct.

## Querying

`pan`'s core abstraction, the point of it, is to turn

```sql
SELECT person_id, fname, lname, age FROM people WHERE person_id = ? AND age > ? LIMIT 10;
```

into a series of expressions, that can be conditionally included with ease:

```go
query := pan.New("SELECT person_id, fname, lname, age FROM people")
if personID != nil {
    query.Where()
    query.Expression("person_id = ?", personID)
}
if age != nil {
    query.Where()
    query.Expression("age > ?", age)
}
query.Flush(" AND ")
```

It was created while writing list endpoints for an API with a handful of optional filter fields on them.
The combinatorial explosion of query strings required was hard to manage, and when building the query string conditionally, it was annoying to keep track of when things like `WHERE` had already been included and couldn't be repeated.
`pan` is the lightest possible abstraction we could find to make it easier to programmatically build query strings.

`pan`'s two major abstractions are the [`Query`](https://pkg.go.dev/impractical.co/pan#Query), which holds the query string being built, and the [`Expression`](https://pkg.go.dev/impractical.co/pan#Query.Expression), which adds a chunk to the `Query`'s buffer.
A `Query` keeps track of the string and also the parameters to include in the string.
A `Query` gets started, has `Expression`s added to it, and at least once has the [`Flush`](https://pkg.go.dev/impractical.co/pan#Query.Flush) method called, which joins the `Expression`s in the buffer by the passed string and adds them to the query string.
The `Flush` is necessary because `Expression`s are buffered before they get added to the query string.
This lets you, like in the example above, join however many `WHERE` clauses (for example) you have by `AND` without needing to handle it manually.

### Using Queries

Once a query has been built, it can be used to generate a SQL string.
The `MySQLString`, `PostgreSQLString`, and `SQLiteString` methods will generate the query string with placeholders appropriate for those particular SQL dialects.
The `String` method will naively include the Go values in place of the placeholders.

> [!CAUTION]
> The `String` method is meant as a debug aid; it is not guaranteed to or even likely to generate valid SQL, and even if the output _were_ valid SQL, it would be a massive security risk to execute it.
> Do not execute the output of `String`!

To execute the query, pass the query string from the `MySQLString`, `PostgreSQLString`, or `SQLiteString` method to your database library of choice, and use the `query.Args()` method as the arguments to include:

```go
query := pan.Insert(myType)
queryString, err := query.PostgreSQLString()
if err != nil {
    panic(err)
}
_, err = db.Exec(queryString, query.Args()...)
if err != nil {
    panic(err)
}
```

### Complex Queries

Sometimes when crafting a query, a CTE or other expression will be required it's nice to be able to spin up a subordinate buffer to the main query.

The [`Query.ComplexExpression`](https://pkg.go.dev/impractical.co/pan#ComplexExpression) method creates a new, independent `Query` with its own buffer and own state around whether, e.g., the `WHERE` clause has been included.
It can then be built on, just like any other `Query`, and when it's ready, the [`Query.AppendToParent`](https://pkg.go.dev/impractical.co/pan#Query.AppendToParent) method can be called, rendering it down to an `Expression` and adding it to its parent `Query`'s buffer.

```go
query := pan.New("WITH avg_salary_by_department AS (")
cte := query.ComplexExpression("SELECT department, AVG(salary) as avg_salary FROM employees")
cte.Where()
cte.Expression("departed = ?", false)
cte.Expression("GROUP BY department")
cte.Flush(" ").AppendToParent()
query.Expression(") SELECT * FROM avg_salary_by_department")
query.Flush(" ")
```

```sql
WITH avg_salary_by_department AS (SELECT department, AVG(salary) as avg_salary FROM employees WHERE departed = ? GROUP BY department) SELECT * FROM avg_salary_by_department;
```

> [!CAUTION]
> `QueryAppendToParent` must not be called on `Query`s that were not created by calling `Query.ComplexExpression`!
> It will panic at runtime if called on a `Query` with no parent.

> [!NOTE]
> Don't forget to call `Query.Flush` before calling `AppendToParent`.

### Helper methods

Some SQL is very common and made sense to write helper methods for.
If you look at the source code of these methods, they are just calls to [`New`](https://pkg.go.dev/impractical.co/pan#New) or [`Expression`](https://pkg.go.dev/impractical.co/pan#Query.Expression) under the hood.

#### [`Select`](https://pkg.go.dev/impractical.co/pan#Select)

```go
query := pan.Select([]string{"column_one", "column_two", "column_three"}, "my_table")
```

`pan.Select` will generate the query to select the specified columns from the specified table:

```sql
SELECT column_one, column_two, column_three FROM my_table;
```

#### [`SelectAll`](https://pkg.go.dev/impractical.co/pan#SelectAll)

```go
query := pan.SelectAll(myType)
```

`pan.SelectAll` will generate the query to select all the columns mapped to the passed struct's fields from the table mapped to that type:

```sql
SELECT column_one, column_two, column_three FROM my_type_table;
```

How the type maps to a table and columns is documented below in the section on type helpers.

#### [`Insert`](https://pkg.go.dev/impractical.co/pan#Insert)

```go
query := pan.Insert(myType)
```

`pan.Insert` will generate the query to insert the values in each of the struct's fields in the column mapped to that field:

```sql
INSERT INTO my_type_table (column_one, column_two, column_three) VALUES (?, ?, ?);
```

How the type maps to a table and columns is documented below in the section on type helpers.

#### [`DeleteFrom`](https://pkg.go.dev/impractical.co/pan#DeleteFrom)

```go
query := pan.DeleteFrom("my_table")
```

`pan.DeleteFrom` will generate the query to delete rows from the passed table:

```sql
DELETE FROM my_table;
```

#### [`Query.Where`](https://pkg.go.dev/impractical.co/pan#Query.Where)

```
query := pan.New("SELECT * FROM table")
query.Where()
query.Expression("foo = ?", true)
query.Where()
query.Expression("bar = ?", false)
query.Flush(" AND ")
```

`Query.Where` will include the `WHERE` keyword, but only if the `WHERE` keyword has not already been included in that query:

```sql
SELECT * FROM table WHERE foo = ? AND bar = ?
```

#### [`Query.Comparison`](https://pkg.go.dev/impractical.co/pan#Query.Comparison)

```go
query := pan.New("SELECT * FROM table")
query.Where()
query.Comparison("foo", "=", true)
```

`Query.Comparison` will include a comparison between a SQL literal (column name or expression) and a Go variable that will be inserted as a parameter:

```sql
SELECT * FROM table WHERE foo = ?;
```

#### [`Query.In`](https://pkg.go.dev/impractical.co/pan#Query.In)

```go
query := pan.New("SELECT * FROM table")
query.Where()
query.In("id", 1, 2, 3)
```

`Query.In` will include an `IN` clause containing a parameter for each Go variable passed:

```sql
SELECT * FROM table WHERE id IN(?, ?, ?);
```

#### [`Query.Assign`](https://pkg.go.dev/impractical.co/pan#Query.Assign)

```go
query := pan.New("UPDATE table SET")
query.Assign("updated_at", time.Now())
```

`Query.Assign` will assign a parameter to a column:

```sql
UPDATE table SET updated_at = ?;
```

#### [`Query.OrderBy`](https://pkg.go.dev/impractical.co/pan#Query.OrderBy)

```go
query := pan.New("SELECT * FROM table")
query.OrderBy("my_column")
query.OrderBy("my_other_column")
```

`Query.OrderBy` will add an `ORDER BY` clause, or add to the existing one:

```sql
SELECT * FROM table ORDER BY my_column, my_other_column;
```

> [!NOTE]
> `pan` does not parse your SQL and operates fundamentally at the level of opaque text.
> The `Query.OrderBy` function will not work as you expect if there are `Expression`s inserted between calls to `Query.OrderBy`. 
> After the first call inserts the `ORDER BY` text, future calls just insert a `,` and whatever value you pass.
> If the preceding expression is not another `ORDER BY` column, the generated SQL will not be what you expect.

#### [`Query.OrderByDesc`](https://pkg.go.dev/impractical.co/pan#Query.OrderByDesc)

```go
query := pan.New("SELECT * FROM table")
query.OrderBy("my_column")
query.OrderByDesc("my_other_column")
```

`Query.OrderByDesc` will add an `ORDER BY ... DESC` clause, or add to the existing `ORDER BY` clause:

```sql
SELECT * FROM table ORDER BY my_column, my_other_column DESC;
```

Regardless of whether `OrderBy` and `OrderByDesc` has been called, only one `ORDER BY` clause will be inserted.

> [!NOTE]
> `pan` does not parse your SQL and operates fundamentally at the level of opaque text.
> The `Query.OrderByDesc` function will not work as you expect if there are `Expression`s inserted between calls to `Query.OrderBy`. 
> After the first call inserts the `ORDER BY` text, future calls just insert a `,` and whatever value you pass.
> If the preceding expression is not another `ORDER BY` column, the generated SQL will not be what you expect.

#### [`Query.Limit`](https://pkg.go.dev/impractical.co/pan#Query.Limit)

```go
query := pan.New("SELECT * FROM table")
query.Limit(10)
```

`Query.Limit` will add the `LIMIT` keyword and use the passed value as a parameter:

```sql
SELECT * FROM table LIMIT ?;
```

#### [`Query.Offset`](https://pkg.go.dev/impractical.co/pan#Query.Offset)

```go
query := pan.New("SELECT * FROM table")
query.Offset(10)
```

`Query.Offset` will add the `OFFSET` keyword and use the passed value as a parameter:

```sql
SELECT * FROM table OFFSET ?;
```

#### [`Placeholders`](https://pkg.go.dev/impractical.co/pan#Placeholders)

```go
query.New("INSERT INTO my_table (column_one, column_two, column_three)")
query.Expression("VALUES ("+pan.Placeholders(3)+")")
```

`pan.Placeholders` will generate the specified number of placeholders and join them with commas:

```sql
INSERT INTO my_table (column_one, column_two, column_three) VALUES (?, ?, ?);
```

## Type Helpers

`pan` includes some interface and reflection-based helpers that make it easier to centralize your column and table names in your application's logic by associating them with a struct type.

Assume the following `Person` struct in your application:

```go
type Person struct {
    ID        int 
    FirstName string
    LastName  string
    Age       int
}
```

And the corresponding `people` table:

```
+-----------+-------------+------+-----+---------+-------+
| Field     | Type        | Null | Key | Default | Extra |
+-----------+-------------+------+-----+---------+-------+
| person_id | int         | NO   |     | 0       |       |
| fname     | varchar(20) | NO   |     | ''      |       |
| lname     | varchar(20) | NO   |     | ''      |       |
| age       | int         | NO   |     | 0       |       |
+-----------+-------------+------+-----+---------+-------+
```

To use that `Person` type with `pan`, you need it to fill the [`SQLTableNamer`](https://pkg.go.dev/impractical.co/pan#SQLTableNamer) interface, letting `pan` know to use the `people` table in your database:

```go
func (p Person) GetSQLTableName() string {
    return "people"
}
```

By default, `pan` maps struct fields to table columns by [snake-casing](https://en.wikipedia.org/wiki/Snake_case) the field name; every capital letter gets lower-cased and preceded by a `_`, unless it was preceded by another capital letter, in which case it's just lowercased.
So for our example struct, the following table columns are expected:

* ID: `id`
* FirstName: `first_name`
* LastName: `last_name`
* Age: `age`

Those don't match our table, so we need to tell `pan` what columns to use, instead.
We can do this with the `sql_column` struct tag:

```go
type Person struct {
    ID        int    `sql_column:"person_id"`
    FirstName string `sql_column:"fname"`
    LastName  string `sql_column:"lname"`
    Age       int
}
```

> [!NOTE]
> Only exported fields are mapped to columns.
> Unexported fields will be ignored.

### Getting the table

To find the table name, you can just call the `GetSQLTableName` method on your type:

```go
table := myType{}.GetSQLTableName()
```

but that's a lot to type.
The [`pan.Table`](https://pkg.go.dev/impractical.co/pan#Table) function does the same thing and is more readable:

```go
table := pan.Table(myType{})
```

### Getting the column for a field

To retrieve the column a specific field is mapped to, use the [`pan.Column`](https://pkg.go.dev/impractical.co/pan#Column) function:

```go
query := pan.SelectAll(Person{})
query.Where()
query.Comparison(pan.Column(Person{}, "ID"), "=", 1)
```

```sql
SELECT person_id, fname, lname, age FROM people WHERE person_id = ?;
```

### Getting all columns for a type

To retrieve all the columns for a type, use the [`pan.Columns`](https://pkg.go.dev/impractical.co/pan#Columns) function:

```go
query := pan.Select([]string(pan.Columns(Person{})), "people")
```

```sql
SELECT person_id, fname, lname, age FROM people;
```

We need to case the return of `pan.Columns` to a `[]string` in the above example because it actually returns [`pan.ColumnList`](https://pkg.go.dev/impractical.co/pan#ColumnList), which uses `[]string` under the hood.
`ColumnList` allows us to set a custom `String()` method, which joins the columns by `, `, which is very convenient when constructing queries.

### Mapping results to a struct

After executing a query, [`pan.Unmarshal`](https://pkg.go.dev/impractical.co/pan#Unmarshal) can map the results into a struct:

```go
mysql, err := query.MySQLString() // could also be PostgreSQLString or SQLiteString
if err != nil {
	// handle the error
}
rows, err := db.Query(mysql, query.Args...)
if err != nil {
	// handle the error
}
defer rows.Close()
var people []Person
for rows.Next() {
	var p Person
    err := pan.Unmarshal(rows, &p) // put the results into the struct
    if err != nil {
        // handle the error
    }
    people = append(people, p)
}
```

The column names associated with the row must match the column names on the struct.
This will always be true if the `SELECT` statement was built using the column-mapping capabilities of `pan`.

## Code generation

While the reflection and interface-based helpers above are useful, it's unfortunate that you need to specify the struct fields as strings.
If there's a typo in the string or a field is renamed, the compiler will not catch it and `pan` will panic at runtime, which is suboptimal.
The optional code generation component of `pan` offers an alternative.

`pan` includes a CLI, also named `pan` at `impractical.co/pan/cmd/pan`.
You can add it to your repository using `go get`:

```sh
go get -tool impractical.co/pan/cmd/pan
```

This CLI has a `generate` command that will parse your package and generate helpers for every `SQLTableNamer` found in the package.
It will create a new type for each `SQLTableNamer`, `columns_YourTypeNameHere`, and a constant `YourTypeNameHereColumns` of that type.
This generated type will have a method for each field on the struct, named for the field on the struct, that returns the name of the column that struct is mapped to.
It will also have a `List` method (unless you have a `List` field, in which case it will fall back on `All`, then `PanList`, then `PanAll`, and finally will just not include this method if you have fields named all of those things) that returns all the columns for the type.
This allows you to do the following:

```go
query := pan.Select(PersonColumns.List(), "people")
query.Where()
query.Comparison(PersonColumns.ID(), "=", 1)
```

If your `SQLTableNamer`'s function body consists _only_ of `return "my_table_name"`, which is almost always the case, it will also generate a `YourTypeNameHereTable` constant holding the table name:

```go
query := pan.Select(PersonColumns.List(), PersonTable)
```

> [!NOTE]
> Your function body must _only_ contain `return "my_table_name"` to have this method generated.
> Because the generator relies on syntax parsing, not the type system, even so much as a comment will make it unable to parse the table name out.

To ensure the generated column names can be used interchangeably with the reflect-based helpers, the generated code will also include a test file with a function that checks that `pan.Columns` returns the same list of columns as `YourTypeNameHereColumns.List()`.
This would only happen if the code generator had not been re-run after changing the struct.

## Column flags

Sometimes, you need more than the column name; you may need the qualified name (`table.column`) or you may be using special characters/need to quote the column name (`"column"` for Postgres, `\`column`\` for MySQL).
To support these use cases, various helper functions take a variable number of flags (including none):

```go
Columns() // returns column format
Columns(FlagFull) // returns table.column format
Columns(FlagDoubleQuoted) // returns "column" format
Columns(FlagTicked) // returns `column` format
Columns(FlagFull, FlagDoubleQuoted) // returns "table"."column" format
Columns(FlagFull, FlagTicked) // returns `table`.`column` format
```

These flags can be used in the following places:

* When calling any method in the generated code.
* `pan.SelectAll`
* `pan.Columns`
* `pan.Column`

If you would like to apply the flagging behavior to a manually-specified column, the [`pan.DecorateColumn`](https://pkg.go.dev/impractical.co/pan#DecorateColumn) will return the passed column with the specified flags applied.
