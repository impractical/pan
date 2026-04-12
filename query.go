package pan

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// FlagFull returns columns in their absolute table.column format.
	FlagFull Flag = iota
	// FlagTicked returns columns using ticks to quote the column name, like `column`.
	FlagTicked
	// FlagDoubleQuoted returns columns using double quotes to quote the column name, like "column".
	FlagDoubleQuoted
)

var (
	// ErrNeedsFlush is returned when a Query is used while it has expressions left in its buffer
	// that haven’t been flushed using the Query’s Flush method.
	ErrNeedsFlush = errors.New("Query has dangling buffer, its Flush method needs to be called")
)

// Query represents an SQL query that is being built. It can be used from its empty value,
// or it can be instantiated with the New method.
//
// Query instances are used to build SQL query string and argument lists, and consist of an
// SQL string and a buffer. The Flush method must be called before the Query is used, or you
// may leave expressions dangling in the buffer.
//
// The Query type is not meant to be concurrency-safe; if you need to modify it from multiple
// goroutines, you need to coordinate that access yourself.
type Query struct {
	sql           string
	args          []any
	expressions   []string
	includesWhere bool
	includesOrder bool
	parent        *Query
}

// ColumnList represents a set of columns.
type ColumnList []string

// String returns the columns in the ColumnList, joined by ", ", often used to create an
// SQL-formatted list of column names.
func (c ColumnList) String() string {
	return strings.Join(c, ", ")
}

// Flag represents a modification to the returned values from our Column or Columns functions.
// See the constants defined in this package for valid values.
type Flag int

// New returns a new Query instance, primed for use.
func New(query string) *Query {
	return &Query{
		sql:  query,
		args: []any{},
	}
}

// Insert returns a Query instance containing SQL that will insert the passed `values` into
// the database.
func Insert[Type SQLTableNamer](values ...Type) *Query {
	columns := Columns(values[0])
	query := New("INSERT INTO " + Table(values[0]) + " (" + columns.String() + ") VALUES")

	for _, v := range values {
		columnValues := ColumnValues(v)
		query.Expression("("+Placeholders(len(columnValues))+")", columnValues...)
	}
	return query.Flush(", ")
}

// WrongNumberArgsError is returned when you’ve generated a Query with a certain number of
// placeholders, but supplied a different number of arguments. The NumExpected property
// holds the number of placeholders in the Query, and the NumFound property holds the
// number of arguments supplied.
type WrongNumberArgsError struct {
	NumExpected int
	NumFound    int
}

// Error fills the error interface.
func (e WrongNumberArgsError) Error() string {
	return fmt.Sprintf("Expected %d arguments, got %d.", e.NumExpected, e.NumFound)
}

func (q *Query) checkCounts() error {
	placeholders := strings.Count(q.sql, "?")
	args := len(q.args)
	if placeholders != args {
		return WrongNumberArgsError{NumExpected: placeholders, NumFound: args}
	}
	return nil
}

// String returns a version of your Query with all the arguments in the place of their
// placeholders. It does not do any sanitization, and is vulnerable to SQL injection.
// It is meant as a debugging aid, not to be executed. The string will almost certainly
// not be valid SQL.
func (q *Query) String() string {
	var argPos int
	var res strings.Builder
	toCheck := q.sql
	for charIndex := strings.Index(toCheck, "?"); charIndex >= 0; argPos++ {
		var arg any
		arg = "!{MISSING}"
		if len(q.args) > argPos {
			arg = q.args[argPos]
		}
		res.WriteString(toCheck[:charIndex])
		fmt.Fprintf(&res, "%v", arg)
		toCheck = toCheck[charIndex+1:]
		charIndex = strings.Index(toCheck, "?")
	}
	res.WriteString(toCheck)
	return res.String()
}

// MySQLString returns a SQL string that can be passed to MySQL to execute your query.
// If the number of placeholders do not match the number of arguments provided to your
// Query, an ErrWrongNumberArgs error will be returned. If there are still expressions
// left in the buffer (meaning the Flush method wasn't called) an ErrNeedsFlush error
// will be returned.
func (q *Query) MySQLString() (string, error) {
	if len(q.expressions) != 0 {
		return "", ErrNeedsFlush
	}
	if err := q.checkCounts(); err != nil {
		return "", err
	}
	return q.sql + ";", nil
}

// SQLiteString returns a SQL string that can be passed to SQLite to execute
// your query. If the number of placeholders do not match the number of
// arguments provided to your Query, an ErrWrongNumberArgs error will be
// returned. If there are still expressions left in the buffer (meaning the
// Flush method wasn't called) an ErrNeedsFlush error will be returned.
func (q *Query) SQLiteString() (string, error) {
	if len(q.expressions) != 0 {
		return "", ErrNeedsFlush
	}
	if err := q.checkCounts(); err != nil {
		return "", err
	}
	return q.sql + ";", nil
}

// PostgreSQLString returns an SQL string that can be passed to PostgreSQL to execute
// your query. If the number of placeholders do not match the number of arguments
// provided to your Query, an ErrWrongNumberArgs error will be returned. If there are
// still expressions left in the buffer (meaning the Flush method wasn't called) an
// ErrNeedsFlush error will be returned.
func (q *Query) PostgreSQLString() (string, error) {
	if len(q.expressions) != 0 {
		return "", ErrNeedsFlush
	}
	if err := q.checkCounts(); err != nil {
		return "", err
	}
	count := 1
	var res strings.Builder
	toCheck := q.sql
	for i := strings.Index(toCheck, "?"); i >= 0; count++ {
		res.WriteString(toCheck[:i])
		res.WriteString("$" + strconv.Itoa(count))
		toCheck = toCheck[i+1:]
		i = strings.Index(toCheck, "?")
	}
	res.WriteString(toCheck)
	return res.String() + ";", nil
}

// ComplexExpression starts a Query with a new buffer, so it can be flushed
// without affecting the outer Query's buffer of expressions.
//
// Once a ComplexExpression has been flushed, it should have its AppendToParent
// method called, which sets the entire ComplexExpression as a single
// expression on its parent Query.
func (q *Query) ComplexExpression(query string) *Query {
	return &Query{
		sql:    query,
		args:   []any{},
		parent: q,
	}
}

// AppendToParent sets the entire Query as a single expression on its parent
// Query. It should only be called on Querys created by calling
// ComplexExpression; calling it on a Query that isn't a ComplexExpression will
// panic.
//
// It returns the parent of the Query.
func (q *Query) AppendToParent() *Query {
	if q.parent == nil {
		panic("can't call AppendToParent on a Query that wasn't created using Query.ComplexExpression")
	}
	if len(q.expressions) > 0 {
		panic(ErrNeedsFlush)
	}
	if err := q.checkCounts(); err != nil {
		panic(err)
	}
	return q.parent.Expression(q.sql, q.args...)
}

// Flush flushes the expressions in the Query’s buffer, adding them to the SQL string
// being built. It must be called before a Query can be used. Any pending expressions
// (anything since the last Flush or since the Query was instantiated) are joined using
// `join`, then added onto the Query’s SQL string, with a space between the SQL string
// and the expressions.
func (q *Query) Flush(join string) *Query {
	if len(q.expressions) < 1 {
		return q
	}
	q.sql = strings.TrimSpace(q.sql) + " "
	q.sql += strings.TrimSpace(strings.Join(q.expressions, join))
	q.expressions = q.expressions[0:0]
	return q
}

// Expression adds a raw string and optional values to the Query’s buffer.
func (q *Query) Expression(key string, values ...any) *Query {
	q.expressions = append(q.expressions, key)
	q.args = append(q.args, values...)
	return q
}

// Where adds a WHERE keyword to the Query’s buffer, then calls Flush on the Query,
// using a space as the join parameter.
//
// Where can only be called once per Query; calling it multiple times on the same Query
// will be no-ops after the first.
func (q *Query) Where() *Query {
	if q.includesWhere {
		return q
	}
	q.Expression("WHERE")
	q.Flush(" ")
	q.includesWhere = true
	return q
}

// Comparison adds a comparison expression to the Query’s buffer. A comparison takes the
// form of `column operator ?`, with `value` added as an argument to the Query. Column is
// determined by finding the column name for the passed property on the passed SQLTableNamer.
// The passed property must be a string that matches, identically, the property name; if it
// does not, it will panic.
func (q *Query) Comparison(column, operator string, value any) *Query {
	return q.Expression(column+" "+operator+" ?", value)
}

// In adds an expression to the Query’s buffer in the form of "column IN (value, value, value)".
// `values` are the variables to match against, and `obj` and `property` are used to determine
// the column. `property` must exactly match the name of a property on `obj`, or the call will
// panic.
func (q *Query) In(column string, values ...any) *Query {
	return q.Expression(column+" IN("+Placeholders(len(values))+")", values...)
}

// Assign adds an expression to the Query’s buffer in the form of "column = ?", and adds `value`
// to the arguments for this query. `obj` and `property` are used to determine the column.
// `property` must exactly match the name of a property on `obj`, or the call will panic.
func (q *Query) Assign(column string, value any) *Query {
	return q.Expression(column+" = ?", value)
}

func (q *Query) orderByWithDir(orderClause, dir string) *Query {
	exp := ", "
	if !q.includesOrder {
		exp = "ORDER BY "
		q.includesOrder = true
	}
	q.Expression(exp + orderClause + dir)
	return q
}

// OrderBy adds an expression to the Query’s buffer in the form of "ORDER BY column".
func (q *Query) OrderBy(column string) *Query {
	return q.orderByWithDir(column, "")
}

// OrderByDesc adds an expression to the Query’s buffer in the form of "ORDER BY column DESC".
func (q *Query) OrderByDesc(column string) *Query {
	return q.orderByWithDir(column, " DESC")
}

// Limit adds an expression to the Query’s buffer in the form of "LIMIT ?", and adds `limit` as
// an argument to the Query.
func (q *Query) Limit(limit int64) *Query {
	return q.Expression("LIMIT ?", limit)
}

// Offset adds an expression to the Query’s buffer in the form of "OFFSET ?", and adds `offset`
// as an argument to the Query.
func (q *Query) Offset(offset int64) *Query {
	return q.Expression("OFFSET ?", offset)
}

// Select starts a new [Query] by selecting the specified columns from the
// specified table.
func Select(columns []string, table string) *Query {
	return New("SELECT " + ColumnList(columns).String() + " FROM " + table)
}

// SelectAll starts a new [Query] by selecting every column associated with a
// field on the passed [SQLTableNamer] from the table mapped to the
// [SQLTableNamer].
//
// The passed [Flag]s can be used to control how columns will be quoted or
// qualified.
func SelectAll(table SQLTableNamer, flags ...Flag) *Query {
	return Select(Columns(table, flags...), Table(table))
}

// DeleteFrom starts a new [Query] that will delete from the table mapped to
// the passed [SQLTableNamer].
func DeleteFrom(table string) *Query {
	return New("DELETE FROM " + table)
}

// Args returns a slice of the arguments attached to the Query, which should be used when executing
// your SQL to fill the placeholders.
//
// Note that Args returns its internal slice; you should copy the returned slice over before modifying
// it.
func (q *Query) Args() []any {
	return q.args
}
