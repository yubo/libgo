package orm

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/yubo/golib/api/errors"
	"k8s.io/klog/v2"
)

var (
	dbFactories       = map[string]DBFactory{}
	DefaultStringSize = 255
	DEBUG             = false
)

type DBFactory func(db DB) Driver

func Register(name string, d DBFactory) {
	if _, ok := dbFactories[name]; ok {
		panic(fmt.Sprintf("db factory %s has been set", name))
	}
	dbFactories[name] = d
	klog.V(3).InfoS("db factory register", "name", name)
}

type ormDB struct {
	*Options
	db *sql.DB // DB

	dbBase
}

func Open(driverName, dataSourceName string, opts ...Option) (DB, error) {
	o := &Options{}
	for _, opt := range append(opts, WithDirver(driverName), WithDsn(dataSourceName)) {
		opt(o)
	}

	if err := o.Validate(); err != nil {
		return nil, err
	}

	return open(o)
}

func open(opts *Options) (DB, error) {
	rawDB, err := sql.Open(opts.driver, opts.dsn)
	if err != nil {
		return nil, err
	}

	if !opts.withoutPing {
		if err := rawDB.Ping(); err != nil {
			rawDB.Close()
			return nil, err
		}
	}

	if opts.ctx != nil {
		go func() {
			<-opts.ctx.Done()
			rawDB.Close()
		}()
	}

	if opts.maxIdleCount != nil {
		rawDB.SetMaxIdleConns(*opts.maxIdleCount)
	}
	if opts.maxOpenConns != nil {
		rawDB.SetMaxOpenConns(*opts.maxOpenConns)
	}
	if opts.connMaxLifetime != nil {
		rawDB.SetConnMaxLifetime(*opts.connMaxLifetime)
	}
	if opts.connMaxIdletime != nil {
		rawDB.SetConnMaxIdleTime(*opts.connMaxIdletime)
	}

	db := &ormDB{
		Options: opts,
		db:      rawDB,
		dbBase: dbBase{
			Options: opts,
			rawDB:   rawDB,
			Driver:  &nonDriver{},
		},
	}

	if f, ok := dbFactories[opts.driver]; ok {
		db.Driver = f(db)
	}

	return db, nil
}

func (p *ormDB) RawDB() *sql.DB {
	return p.db
}

func (p *ormDB) Close() error {
	return p.db.Close()
}

func (p *ormDB) Begin() (Tx, error) {
	return p.BeginTx(context.Background(), nil)
}

func (p *ormDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error) {
	tx, err := p.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &ormTx{tx: tx,
		dbBase: dbBase{
			Options: p.Options,
			rawDB:   tx,
		},
	}, nil
}

func (p *ormDB) ExecRows(bytes []byte) (err error) {
	var cmds []string
	var tx *sql.Tx

	if tx, err = p.db.Begin(); err != nil {
		return fmt.Errorf("Begin() err: %s", err)
	}

	defer func() {
		if err != nil {
			tx.Rollback()
		} else {
			tx.Commit()
		}
	}()

	lines := strings.Split(string(bytes), "\n")
	for cmd, in, i := "", false, 0; i < len(lines); i++ {
		line := lines[i]
		if len(line) == 0 || strings.HasPrefix(line, "-- ") {
			continue
		}

		if in {
			cmd += " " + strings.TrimSpace(line)
			if cmd[len(cmd)-1] == ';' {
				cmds = append(cmds, cmd)
				in = false
			}
		} else {
			n := strings.Index(line, " ")
			if n <= 0 {
				continue
			}

			switch line[:n] {
			case "SET", "CREATE", "INSERT", "DROP":
				cmd = line
				if line[len(line)-1] == ';' {
					cmds = append(cmds, cmd)
				} else {
					in = true
				}
			}
		}
	}

	for i := 0; i < len(cmds); i++ {
		_, err := tx.Exec(cmds[i])
		if err != nil {
			klog.V(3).Infof("%v", err)
			return fmt.Errorf("sql %s\nerr %s", cmds[i], err)
		}
	}
	return nil
}

type ormTx struct {
	*Options
	tx *sql.Tx

	dbBase
}

func (p *ormTx) Tx() *sql.Tx {
	return p.tx
}

func (p *ormTx) Rollback() error {
	return p.tx.Rollback()
}

func (p *ormTx) Commit() error {
	return p.tx.Commit()
}

type Rows struct {
	*Options
	db    Interface
	query string
	args  []interface{}

	maxRows int
	rows    *sql.Rows
	b       *binder
	err     error
}

// Row(*int, *int, ...)
// Row(*struct{})
// Row(**struct{})
func (p *Rows) Row(dst ...interface{}) error {
	if p.err != nil {
		return p.err
	}
	defer p.rows.Close()

	if p.rows.Next() {
		if len(dst) == 1 && isStructMode(dst[0]) {
			return p.scanRow(dst[0])
		}

		return p.rows.Scan(dst...)
	}

	if !p.ignoreNotFound {
		return errors.NewNotFound("object")
	}

	return nil
}

// scanRow scan row result into dst struct
// dst must be struct, should be prechecked by isStructMode()
func (p *Rows) scanRow(dst interface{}) error {
	row := reflect.Indirect(reflect.ValueOf(dst))

	if !row.CanSet() {
		return fmt.Errorf("scan target can not be set")
	}

	b, err := p.genBinder(row.Type())
	if err != nil {
		return err
	}

	if err := b.scan(row); err != nil {
		return fmt.Errorf("rows.scan() err: %s", err)
	}

	return nil
}

// Rows([]struct{})
// Rows([]*struct{})
// Rows(*[]struct{})
// Rows(*[]*struct{})
// Rows([]string)
// Rows([]*string)
// Rows ignore notfound err msg
func (p *Rows) Rows(dst interface{}) error {
	if p.err != nil {
		return p.err
	}
	defer p.rows.Close()

	limit := p.maxRows

	rv, err := rowsInputValue(dst)
	if err != nil {
		return err
	}

	// sample is slice elem type
	sample := rv.Type().Elem()
	n := 0

	if !isStructMode(reflect.New(sample).Interface()) {
		// e.g. []string or []*string
		for p.rows.Next() {
			row := reflect.New(sample).Elem()

			if err := p.rows.Scan(row.Addr().Interface()); err != nil {
				return fmt.Errorf("rows.scan() err: %s", err)
			}

			rv.Set(reflect.Append(rv, row))

			if n += 1; n >= limit {
				break
			}
		}
		return nil
	}

	// elem is struct
	b, err := p.genBinder(reflect.New(sample).Elem().Type())
	if err != nil {
		return err
	}

	for p.rows.Next() {
		row := reflect.New(sample).Elem()
		b.scan(row)
		rv.Set(reflect.Append(rv, row))

		if n += 1; n >= limit {
			break
		}
	}
	return nil
}

func rowsInputValue(sample interface{}) (rv reflect.Value, err error) {
	rv = reflect.Indirect(reflect.ValueOf(sample))

	if !rv.CanSet() {
		return rv, fmt.Errorf("scan target can not be set")
	}

	// for *[]struct{}
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return rv, fmt.Errorf("needs a pointer to a slice")
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Slice {
		return rv, fmt.Errorf("needs a pointer to a slice")
	}

	return rv, nil
}

func (p *Rows) genBinder(rt reflect.Type) (*binder, error) {
	if p.rows == nil {
		return nil, fmt.Errorf("rows is nil")
	}

	fields, err := p.rows.Columns()
	if err != nil {
		return nil, err
	}

	fieldMap := map[string]int{}
	for i, name := range fields {
		fieldMap[strings.ToLower(name)] = i
	}

	var empty interface{}
	dest := make([]interface{}, len(fields))
	for i := 0; i < len(dest); i++ {
		dest[i] = &empty
	}

	// klog.Infof("dest len %d", len(dest))
	return &binder{
		fields:   cachedTypeFields(rt, p.db),
		dest:     dest,
		fieldMap: fieldMap,
		rows:     p.rows,
	}, nil

}

type binder struct {
	fields   structFields
	dest     []interface{}
	fieldMap map[string]int
	rows     *sql.Rows
}

func (p binder) scan(sample reflect.Value) error {
	tran, err := p.bind(sample)
	if err != nil {
		return err
	}

	if err := p.rows.Scan(p.dest...); err != nil {
		return fmt.Errorf("Scan() err: %s", err)
	}

	for _, v := range tran {
		if err := v.unmarshal(); err != nil {
			return err
		}
	}

	return nil
}

type transfer struct {
	dstProxy interface{} // byte
	dst      interface{} // raw
	ptr      bool
}

// json -> dst
func (p *transfer) unmarshal() error {
	if p.dstProxy == nil {
		return nil
	}

	rv := reflect.Indirect(reflect.ValueOf(p.dst))
	if p.ptr {
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		rv = rv.Elem()
	}

	// TODO: time.Time
	if i, ok := p.dstProxy.(int64); ok {
		t := time.Unix(i, 0)
		if dst, ok := rv.Addr().Interface().(*time.Time); ok {
			*dst = t
		}
		return nil
	}

	if jsonStr, ok := p.dstProxy.([]byte); ok {
		if err := json.Unmarshal(jsonStr, rv.Addr().Interface()); err != nil {
			dlog(2, "json.Unmarshal() error %s", err)
		}
	}

	return nil
}

func (p *binder) bind(rv reflect.Value) ([]*transfer, error) {
	tran := []*transfer{}
	for _, f := range p.fields.list {
		if i, ok := p.fieldMap[f.name]; ok {
			fv, err := getSubv(rv, f.index, true)
			if err != nil {
				return nil, err
			}
			if p.dest[i], err = scanInterface(fv, &tran); err != nil {
				return nil, err
			}
		}
	}

	return tran, nil
}

type RowsIter interface {
	Close() error
	Next() bool
	Row(dest ...interface{}) error
}

func (p *Rows) Iterator() (RowsIter, error) {
	if p.err != nil {
		return nil, p.err
	}

	return &rowsIterator{Rows: p}, nil
}

type rowsIterator struct {
	*Rows
}

func (p *rowsIterator) Close() error {
	return p.rows.Close()
}

func (p *rowsIterator) Next() bool {
	return p.rows.Next()
}

func (p *rowsIterator) Row(dst ...interface{}) error {
	if p.err != nil {
		return p.err
	}

	if len(dst) == 1 && isStructMode(dst[0]) {
		return p.Rows.scanRow(dst[0])
	}

	return p.rows.Scan(dst...)
}
