// Package pan provides a structured way to build a SQL query and map tables to
// structs.
//
// pan views SQL queries as collection of expressions, and allows you to build
// those collections while conditionally including or excluding individual
// expressions. It is notably not an ORM; it makes no attempt to make objects
// the center of your interactions with your database. It instead offers
// affordances for managing the query strings themselves, as a first-class
// abstraction.
//
// pan also, as a matter of convenience, defines the [SQLTableNamer] type,
// which structs can implement to map their fields to specific database
// columns, and make it easier to parse a query's results into a struct. This
// is entirely optional, however.
//
// Finally, the pan cmd found in impractical.co/pan/cmd/pan can be used to
// generate helpers for retrieving a struct field's column name at compile
// time. This offers compiler and language server assistance that the
// reflection-based [Column] method cannot. It is also entirely optional.
package pan
