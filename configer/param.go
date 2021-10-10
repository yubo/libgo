package configer

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/cast"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
)

type param struct {
	envName      string      // env name
	flag         string      // flag
	shothand     string      // flag shothand
	configPath   string      // config path
	flagValue    interface{} // flag's value
	defaultValue interface{} // flag's default value
}

func pathValueToTable(path string, val interface{}) map[string]interface{} {
	paths := parsePath(path)
	p := val

	for i := len(paths) - 1; i >= 0; i-- {
		p = map[string]interface{}{paths[i]: p}
	}
	return p.(map[string]interface{})
}

func (p *Configer) Envs() (names []string) {
	if !p.enableEnv {
		return
	}
	for _, f := range p.params {
		if f.envName != "" {
			names = append(names, f.envName)
		}
	}
	return
}

func (p *Configer) Flags() (names []string) {
	if !p.enableFlag {
		return
	}
	for _, f := range p.params {
		if f.flag != "" {
			names = append(names, f.flag)
		}
	}
	return
}

func (p *Configer) mergeDefaultValues(into map[string]interface{}) {
	for _, f := range p.params {
		if v := f.defaultValue; v != nil {
			klog.V(7).InfoS("def", "path", joinPath(append(p.path, f.configPath)...), "value", v)
			mergeValues(into, pathValueToTable(joinPath(append(p.path, f.configPath)...), v))
		}
	}
}

func (p *Configer) mergeFlagValues(into map[string]interface{}) {
	if !p.enableFlag {
		return
	}
	for _, f := range p.params {
		if v := p.getFlagValue(f); v != nil {
			klog.V(7).InfoS("flag", "path", joinPath(append(p.path, f.configPath)...), "value", v)
			mergeValues(into, pathValueToTable(joinPath(append(p.path, f.configPath)...), v))
		}
	}
}

func (p *Configer) getFlagValue(f *param) interface{} {
	if f.flag == "" {
		return nil
	}

	if p.flagSet.Changed(f.flag) {
		return reflect.ValueOf(f.flagValue).Elem().Interface()
	}

	return nil
}

func (p *options) addConfigs(path []string, fs *pflag.FlagSet, rt reflect.Type) error {
	if len(path) > p.maxDepth {
		return fmt.Errorf("path.depth is larger than the maximum allowed depth of %d", p.maxDepth)
	}

	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		isUnexported := sf.PkgPath != ""
		if sf.Anonymous {
			t := sf.Type
			if t.Kind() == reflect.Ptr {
				t = t.Elem()
			}
			if isUnexported && t.Kind() != reflect.Struct {
				// Ignore embedded fields of unexported non-struct types.
				continue
			}
		} else if isUnexported {
			// Ignore unexported non-embedded fields.
			continue
		}

		opt := p.getTagOpts(sf, path)
		if opt.Skip {
			continue
		}

		ft := sf.Type
		if ft.Kind() == reflect.Ptr {
			// Follow pointer.
			ft = ft.Elem()
		}

		if ft.Kind() == reflect.Struct {
			if opt.Json == "" {
				// anonymous
				if err := p.addConfigs(path, fs, ft); err != nil {
					return err
				}
				continue
			}

			if err := p.addConfigs(append(path, opt.Json), fs, ft); err != nil {
				return err
			}
			continue
		}

		ps := strings.Join(append(path, opt.Json), ".")
		def := opt.Default

		switch t := reflect.New(ft).Elem().Interface(); t.(type) {
		case bool:
			addConfigField(fs, ps, opt, fs.Bool, fs.BoolP, cast.ToBool(def))
		case string:
			addConfigField(fs, ps, opt, fs.String, fs.StringP, cast.ToString(def))
		case int32, int16, int8, int:
			addConfigField(fs, ps, opt, fs.Int, fs.IntP, cast.ToInt(def))
		case int64:
			addConfigField(fs, ps, opt, fs.Int64, fs.Int64P, cast.ToInt64(def))
		case uint:
			addConfigField(fs, ps, opt, fs.Uint, fs.UintP, cast.ToUint(def))
		case uint8:
			addConfigField(fs, ps, opt, fs.Uint8, fs.Uint8P, cast.ToUint8(def))
		case uint16:
			addConfigField(fs, ps, opt, fs.Uint8, fs.Uint8P, cast.ToUint16(def))
		case uint32:
			addConfigField(fs, ps, opt, fs.Uint32, fs.Uint32P, cast.ToUint32(def))
		case uint64:
			addConfigField(fs, ps, opt, fs.Uint64, fs.Uint64P, cast.ToUint64(def))
		case float32, float64:
			addConfigField(fs, ps, opt, fs.Float64, fs.Float64P, cast.ToFloat64(def))
		case time.Duration:
			addConfigField(fs, ps, opt, fs.Duration, fs.DurationP, cast.ToDuration(def))
		case []string:
			addConfigField(fs, ps, opt, fs.StringArray, fs.StringArrayP, cast.ToStringSlice(def))
		case []int:
			addConfigField(fs, ps, opt, fs.IntSlice, fs.IntSliceP, cast.ToIntSlice(def))
		case map[string]string:
			addConfigField(fs, ps, opt, fs.StringToString, fs.StringToStringP, cast.ToStringMapString(def))
		default:
			klog.V(6).InfoS("add config unsupported", "type", ft.String(), "path", joinPath(path...), "kind", ft.Kind())
		}
	}
	return nil
}

func (p *options) getTagOpts(sf reflect.StructField, paths []string) *TagOpts {
	if p.tagOptsGetter != nil {
		field := strings.TrimPrefix(joinPath(append(paths, sf.Name)...), p.prefixPath+".")
		if opts := p.tagOptsGetter(field); opts != nil {
			return opts
		}
	}
	return getTagOpts(sf, p)
}

type TagOpts struct {
	Name        string   // field name
	Json        string   // json:"{json}"
	Flag        []string // flag:"{long},{short}"
	Default     string   // default:"{default}"
	Env         string   // env:"{env}"
	Description string   // description:"{description}"
	Skip        bool     // if json:"-"
	Arg         string   // arg:"{arg}"  args[0] arg1... -- arg2...
}

func (p TagOpts) String() string {
	return fmt.Sprintf("json %s flag %v env %s description %s",
		p.Json, p.Flag, p.Env, p.Description)
}

func getTagOpts(sf reflect.StructField, o *options) (tag *TagOpts) {
	tag = &TagOpts{Name: sf.Name}
	if sf.Anonymous {
		return
	}

	json, _ := parseTag(sf.Tag.Get("json"))
	if json == "-" {
		tag.Skip = true
		return
	}

	if json != "" {
		tag.Json = json
	}

	if flag := strings.Split(strings.TrimSpace(sf.Tag.Get("flag")), ","); len(flag) > 0 && flag[0] != "" && flag[0] != "-" {
		tag.Flag = flag
	}

	tag.Default = sf.Tag.Get("default")
	tag.Description = sf.Tag.Get("description")
	tag.Arg = sf.Tag.Get("arg")
	tag.Env = strings.Replace(strings.ToUpper(sf.Tag.Get("env")), "-", "_", -1)

	if !o.enableEnv || tag.Env == "" {
		return
	}

	tag.Description = fmt.Sprintf("%s (env %s)", tag.Description, tag.Env)

	v, ok := o.getEnv(tag.Env)
	if !ok {
		return
	}

	tag.Default = v
	return
}

// tagOptions is the string following a comma in a struct field's "json"
// tag, or the empty string. It does not include the leading comma.
type tagOptions string

// parseTag splits a struct field's json tag into its name and
// comma-separated options.
func parseTag(tag string) (string, tagOptions) {
	if idx := strings.Index(tag, ","); idx != -1 {
		return tag[:idx], tagOptions(tag[idx+1:])
	}
	return tag, tagOptions("")
}

// Contains reports whether a comma-separated list of options
// contains a particular substr flag. substr must be surrounded by a
// string boundary or commas.
func (o tagOptions) Contains(optionName string) bool {
	if len(o) == 0 {
		return false
	}
	s := string(o)
	for s != "" {
		var next string
		i := strings.Index(s, ",")
		if i >= 0 {
			s, next = s[:i], s[i+1:]
		}
		if s == optionName {
			return true
		}
		s = next
	}
	return false
}

func addConfigField(fs *pflag.FlagSet, path string, opt *TagOpts, varFn, varPFn, def interface{}) {
	v := &param{
		configPath: path,
		envName:    opt.Env,
	}

	if opt.Default != "" {
		v.defaultValue = def
	}

	// add flag
	switch len(opt.Flag) {
	case 0:
		// nothing
	case 1:
		v.flag = opt.Flag[0]
		ret := reflect.ValueOf(varFn).Call([]reflect.Value{
			reflect.ValueOf(opt.Flag[0]),
			reflect.ValueOf(def),
			reflect.ValueOf(opt.Description),
		})
		v.flagValue = ret[0].Interface()
	case 2:
		v.flag = opt.Flag[0]
		v.shothand = opt.Flag[1]
		ret := reflect.ValueOf(varPFn).Call([]reflect.Value{
			reflect.ValueOf(opt.Flag[0]),
			reflect.ValueOf(opt.Flag[1]),
			reflect.ValueOf(def),
			reflect.ValueOf(opt.Description),
		})
		v.flagValue = ret[0].Interface()
	default:
		panic("invalid flag value")
	}

	configerOptions.params = append(configerOptions.params, v)
}
