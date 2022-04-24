package orm

import (
	"context"
	"reflect"
	"time"

	"github.com/yubo/golib/api/errors"
	"github.com/yubo/golib/queries"
	"github.com/yubo/golib/util"
)

type DBOptions struct {
	ctx context.Context
	//greatest        string
	driver          string
	dsn             string
	ignoreNotFound  bool
	withoutPing     bool
	maxRows         int
	maxIdleCount    *int
	maxOpenConns    *int
	connMaxLifetime *time.Duration
	connMaxIdletime *time.Duration
	stringSize      int
	err             error
}

func NewDefaultDBOptions() *DBOptions {
	return &DBOptions{
		maxRows:    1000,
		stringSize: 255,
	}
}

type DBOption func(*DBOptions)

func (p *DBOptions) Validate() error {
	return p.err
}

func WithContext(ctx context.Context) DBOption {
	return func(o *DBOptions) {
		o.ctx = ctx
	}
}

func WithIgnoreNotFound() DBOption {
	return func(o *DBOptions) {
		o.ignoreNotFound = true
	}
}

func WithoutPing() DBOption {
	return func(o *DBOptions) {
		o.withoutPing = true
	}
}

func WithDirver(driver string) DBOption {
	return func(o *DBOptions) {
		o.driver = driver
	}
}

func WithMaxRows(n int) DBOption {
	return func(o *DBOptions) {
		o.maxRows = n
	}
}

func WithDsn(dsn string) DBOption {
	return func(o *DBOptions) {
		o.dsn = dsn
	}
}

func WithMaxIdleCount(n int) DBOption {
	return func(o *DBOptions) {
		o.maxIdleCount = &n
	}
}

func WithMaxOpenConns(n int) DBOption {
	return func(o *DBOptions) {
		o.maxOpenConns = &n
	}
}

func WithConnMaxLifetime(d time.Duration) DBOption {
	return func(o *DBOptions) {
		o.connMaxLifetime = &d
	}
}

func WithConnMaxIdletime(d time.Duration) DBOption {
	return func(o *DBOptions) {
		o.connMaxIdletime = &d
	}
}

func NewOptions(opts ...Option) (*Options, error) {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	return o, o.err
}

type Options struct {
	err            error
	sample         interface{}
	total          *int64
	table          string
	tableOptions   []string
	selector       queries.Selector
	cols           []string
	orderby        []string
	offset         *int64
	limit          *int64
	ignoreNotFound bool
}

func (o *Options) Error(err error) error {
	if err != nil && o.ignoreNotFound && errors.IsNotFound(err) {
		return nil
	}

	return err
}

func (o *Options) Sample() interface{} {
	return o.sample
}

type Option func(*Options)

func WithTable(table string) Option {
	return func(o *Options) {
		o.table = table
	}
}

// for automigrate, add to `create table xxx () {tableoptions}`
func WithTableOptions(options ...string) Option {
	return func(o *Options) {
		o.tableOptions = append(o.tableOptions, options...)
	}
}

func WithIgnoreNotFoundErr() Option {
	return func(o *Options) {
		o.ignoreNotFound = true
	}
}

func WithTotal(total *int64) Option {
	return func(o *Options) {
		o.total = total
	}
}

func WithSelector(selector string) Option {
	return func(o *Options) {
		o.selector, o.err = queries.Parse(selector)
	}
}

func WithLimit(offset, limit int64) Option {
	return func(o *Options) {
		o.offset = &offset
		o.limit = &limit
	}
}

func WithCols(cols ...string) Option {
	return func(o *Options) {
		o.cols = cols
	}
}

func WithOrderby(orderby ...string) Option {
	return func(o *Options) {
		o.orderby = orderby
	}
}

func WithSample(sample interface{}) Option {
	return func(o *Options) {
		o.sample = sample
	}
}

func (p *Options) Table() string {
	if p.table != "" {
		return p.table
	}

	rt := reflect.TypeOf(p.sample)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	p.table = util.SnakeCasedName(rt.Name())
	return p.table
}

func (p *Options) GenListSql() (query, countQuery string, args []interface{}, err error) {
	return GenListSql(p.Table(), p.cols, p.selector, p.orderby, p.offset, p.limit)
}

func (p *Options) GenGetSql() (string, []interface{}, error) {
	return GenGetSql(p.Table(), p.cols, p.selector)
}

func (p *Options) GenUpdateSql(db Driver) (string, []interface{}, error) {
	return GenUpdateSql(p.Table(), p.sample, db, p.selector)
}

// TODO: generate selector from sample.fields, like GenUpdateSql
func (p *Options) GenDeleteSql() (string, []interface{}, error) {
	return GenDeleteSql(p.Table(), p.selector)
}

func (p *Options) GenInsertSql(db Driver) (string, []interface{}, error) {
	return GenInsertSql(p.Table(), p.sample, db)
}
