package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	opentracing "github.com/opentracing/opentracing-go"
)

var (
	re = regexp.MustCompile(`[?](\w+)`)
)

type Option func(*DB)

func CacheMapperOption(im IMapper) Option {
	return func(db *DB) {
		db.Mapper = im
	}
}

func EnableTraceOption(enable bool) Option {
	return func(db *DB) {
		db.enableTrace = enable
	}
}

type DB struct {
	*sql.DB
	Mapper      IMapper
	enableTrace bool
}

func Open(driverName, dataSourceName string, opts ...Option) (*DB, error) {
	db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}
	return FromDB(db, opts...), nil
}

func FromDB(db *sql.DB, opts ...Option) *DB {
	xdb := &DB{DB: db, Mapper: NewCacheMapper(&SnakeMapper{})}
	for _, opt := range opts {
		opt(xdb)
	}
	return xdb
}

func (db *DB) Query(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	if db.enableTrace {
		span := newClientSpanFromContext(ctx, query)
		defer span.Finish()
	}

	rows, err := db.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows, db.Mapper}, nil
}

func (db *DB) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if db.enableTrace {
		span := newClientSpanFromContext(ctx, query)
		defer span.Finish()
	}

	return db.DB.ExecContext(ctx, query, args...)
}

func (db *DB) QueryMap(ctx context.Context, query string, mp interface{}) (*Rows, error) {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return nil, err
	}
	return db.Query(ctx, query, args...)
}

func (db *DB) QueryStruct(ctx context.Context, query string, st interface{}) (*Rows, error) {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return nil, err
	}
	return db.Query(ctx, query, args...)
}

func (db *DB) QueryRow(ctx context.Context, query string, args ...interface{}) *Row {
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return &Row{nil, err}
	}
	return &Row{rows, nil}
}

func (db *DB) QueryRowMap(ctx context.Context, query string, mp interface{}) *Row {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return &Row{nil, err}
	}
	return db.QueryRow(ctx, query, args...)
}

func (db *DB) QueryRowStruct(ctx context.Context, query string, st interface{}) *Row {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return &Row{nil, err}
	}
	return db.QueryRow(ctx, query, args...)
}

type Stmt struct {
	*sql.Stmt
	Mapper      IMapper
	names       map[string]int
	enableTrace bool
}

func (db *DB) Prepare(ctx context.Context, query string) (*Stmt, error) {
	names := make(map[string]int)
	var i int
	query = re.ReplaceAllStringFunc(query, func(src string) string {
		names[src[1:]] = i
		i += 1
		return "?"
	})

	if db.enableTrace {
		span := newClientSpanFromContext(ctx, query)
		defer span.Finish()
	}
	stmt, err := db.DB.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &Stmt{stmt, db.Mapper, names, db.enableTrace}, nil
}

func (s *Stmt) Exec(ctx context.Context, args ...interface{}) (sql.Result, error) {
	if s.enableTrace {
		span := newClientSpanFromContext(ctx, "Stmt Exec")
		defer span.Finish()
	}

	return s.Stmt.ExecContext(ctx, args...)
}

func (s *Stmt) ExecMap(ctx context.Context, mp interface{}) (sql.Result, error) {
	vv := reflect.ValueOf(mp)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Map {
		return nil, errors.New("mp should be a map's pointer")
	}

	args := make([]interface{}, len(s.names))
	for k, i := range s.names {
		args[i] = vv.Elem().MapIndex(reflect.ValueOf(k)).Interface()
	}
	return s.Exec(ctx, args...)
}

func (s *Stmt) ExecStruct(ctx context.Context, st interface{}) (sql.Result, error) {
	vv := reflect.ValueOf(st)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Struct {
		return nil, errors.New("mp should be a map's pointer")
	}

	args := make([]interface{}, len(s.names))
	for k, i := range s.names {
		args[i] = vv.Elem().FieldByName(k).Interface()
	}
	return s.Exec(ctx, args...)
}

func (s *Stmt) Query(ctx context.Context, args ...interface{}) (*Rows, error) {
	if s.enableTrace {
		span := newClientSpanFromContext(ctx, "Stmt Query")
		defer span.Finish()
	}

	rows, err := s.Stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows, s.Mapper}, nil
}

func (s *Stmt) QueryMap(ctx context.Context, mp interface{}) (*Rows, error) {
	vv := reflect.ValueOf(mp)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Map {
		return nil, errors.New("mp should be a map's pointer")
	}

	args := make([]interface{}, len(s.names))
	for k, i := range s.names {
		args[i] = vv.Elem().MapIndex(reflect.ValueOf(k)).Interface()
	}

	return s.Query(ctx, args...)
}

func (s *Stmt) QueryStruct(ctx context.Context, st interface{}) (*Rows, error) {
	vv := reflect.ValueOf(st)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Struct {
		return nil, errors.New("mp should be a map's pointer")
	}

	args := make([]interface{}, len(s.names))
	for k, i := range s.names {
		args[i] = vv.Elem().FieldByName(k).Interface()
	}

	return s.Query(ctx, args...)
}

func (s *Stmt) QueryRow(ctx context.Context, args ...interface{}) *Row {
	rows, err := s.Query(ctx, args...)
	return &Row{rows, err}
}

func (s *Stmt) QueryRowMap(ctx context.Context, mp interface{}) *Row {
	vv := reflect.ValueOf(mp)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Map {
		return &Row{nil, errors.New("mp should be a map's pointer")}
	}

	args := make([]interface{}, len(s.names))
	for k, i := range s.names {
		args[i] = vv.Elem().MapIndex(reflect.ValueOf(k)).Interface()
	}

	return s.QueryRow(ctx, args...)
}

func (s *Stmt) QueryRowStruct(ctx context.Context, st interface{}) *Row {
	vv := reflect.ValueOf(st)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Struct {
		return &Row{nil, errors.New("st should be a struct's pointer")}
	}

	args := make([]interface{}, len(s.names))
	for k, i := range s.names {
		args[i] = vv.Elem().FieldByName(k).Interface()
	}

	return s.QueryRow(ctx, args...)
}

// insert into (name) values (?)
// insert into (name) values (?name)
func (db *DB) ExecMap(ctx context.Context, query string, mp interface{}) (sql.Result, error) {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return nil, err
	}

	return db.Exec(ctx, query, args...)
}

func (db *DB) ExecStruct(ctx context.Context, query string, st interface{}) (sql.Result, error) {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return nil, err
	}
	return db.Exec(ctx, query, args...)
}

type EmptyScanner struct {
}

func (EmptyScanner) Scan(src interface{}) error {
	return nil
}

type Tx struct {
	*sql.Tx
	Mapper      IMapper
	enableTrace bool
}

func (db *DB) Begin() (*Tx, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{tx, db.Mapper, db.enableTrace}, nil
}

func (tx *Tx) Prepare(ctx context.Context, query string) (*Stmt, error) {
	names := make(map[string]int)
	var i int
	query = re.ReplaceAllStringFunc(query, func(src string) string {
		names[src[1:]] = i
		i += 1
		return "?"
	})

	if tx.enableTrace {
		span := newClientSpanFromContext(ctx, query)
		defer span.Finish()
	}

	stmt, err := tx.Tx.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &Stmt{stmt, tx.Mapper, names, tx.enableTrace}, nil
}

func (tx *Tx) Stmt(stmt *Stmt) *Stmt {
	// TODO:
	return stmt
}

func (tx *Tx) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if tx.enableTrace {
		span := newClientSpanFromContext(ctx, query)
		defer span.Finish()
	}

	return tx.Tx.ExecContext(ctx, query, args...)
}

func (tx *Tx) ExecMap(ctx context.Context, query string, mp interface{}) (sql.Result, error) {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return nil, err
	}
	return tx.Exec(ctx, query, args...)
}

func (tx *Tx) ExecStruct(ctx context.Context, query string, st interface{}) (sql.Result, error) {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return nil, err
	}
	return tx.Exec(ctx, query, args...)
}

func (tx *Tx) Query(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	if tx.enableTrace {
		span := newClientSpanFromContext(ctx, query)
		defer span.Finish()
	}

	rows, err := tx.Tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows, tx.Mapper}, nil
}

func (tx *Tx) QueryMap(ctx context.Context, query string, mp interface{}) (*Rows, error) {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return nil, err
	}
	return tx.Query(ctx, query, args...)
}

func (tx *Tx) QueryStruct(ctx context.Context, query string, st interface{}) (*Rows, error) {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return nil, err
	}
	return tx.Query(ctx, query, args...)
}

func (tx *Tx) QueryRow(ctx context.Context, query string, args ...interface{}) *Row {
	rows, err := tx.Query(ctx, query, args...)
	return &Row{rows, err}
}

func (tx *Tx) QueryRowMap(ctx context.Context, query string, mp interface{}) *Row {
	query, args, err := MapToSlice(query, mp)
	if err != nil {
		return &Row{nil, err}
	}
	return tx.QueryRow(ctx, query, args...)
}

func (tx *Tx) QueryRowStruct(ctx context.Context, query string, st interface{}) *Row {
	query, args, err := StructToSlice(query, st)
	if err != nil {
		return &Row{nil, err}
	}
	return tx.QueryRow(ctx, query, args...)
}

func MapToSlice(query string, mp interface{}) (string, []interface{}, error) {
	vv := reflect.ValueOf(mp)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Map {
		return "", []interface{}{}, ErrNoMapPointer
	}

	args := make([]interface{}, 0, len(vv.Elem().MapKeys()))
	var err error
	query = re.ReplaceAllStringFunc(query, func(src string) string {
		v := vv.Elem().MapIndex(reflect.ValueOf(src[1:]))
		if !v.IsValid() {
			err = fmt.Errorf("map key %s is missing", src[1:])
		} else {
			args = append(args, v.Interface())
		}
		return "?"
	})

	return query, args, err
}

func StructToSlice(query string, st interface{}) (string, []interface{}, error) {
	vv := reflect.ValueOf(st)
	if vv.Kind() != reflect.Ptr || vv.Elem().Kind() != reflect.Struct {
		return "", []interface{}{}, ErrNoStructPointer
	}

	args := make([]interface{}, 0)
	var err error
	query = re.ReplaceAllStringFunc(query, func(src string) string {
		fv := vv.Elem().FieldByName(src[1:]).Interface()
		if v, ok := fv.(driver.Valuer); ok {
			var value driver.Value
			value, err = v.Value()
			if err != nil {
				return "?"
			}
			args = append(args, value)
		} else {
			args = append(args, fv)
		}
		return "?"
	})
	if err != nil {
		return "", []interface{}{}, err
	}
	return query, args, nil
}

func newClientSpanFromContext(ctx context.Context, query string) opentracing.Span {
	var parentSpanContext opentracing.SpanContext
	if parent := opentracing.SpanFromContext(ctx); parent != nil {
		parentSpanContext = parent.Context()
	}
	// https://github.com/openzipkin/brave/blob/master/instrumentation/mysql/src/main/java/brave/mysql/TracingStatementInterceptor.java#L40
	operationName := query
	if index := strings.Index(operationName, " "); index > 0 {
		operationName = operationName[:index]
	}
	span := opentracing.StartSpan(operationName, opentracing.ChildOf(parentSpanContext))
	span.SetTag("sql.query", query)
	return span
}
