package configer

import (
	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"
)

func newOptions() *Options {
	return &Options{
		enableFlag:    true,
		enableEnv:     true,
		allowEmptyEnv: false,
		maxDepth:      5,
	}
}

type Options struct {
	pathsBase     map[string]string // data in yaml format with path
	pathsOverride map[string]string // data in yaml format with path
	valueFiles    []string          // files, -f/--values
	values        []string          // values, --set
	stringValues  []string          // values, --set-string
	fileValues    []string          // values from file, --set-file=rsaPubData=/etc/ssh/ssh_host_rsa_key.pub
	enableFlag    bool
	enableEnv     bool
	maxDepth      int
	allowEmptyEnv bool
	flagSet       *pflag.FlagSet
	params        []*param // all of config fields
}

func (s *Options) SetOptions(enableEnv, allowEmptyEnv bool, maxDepth int, fs *pflag.FlagSet) {
	s.enableEnv = enableEnv
	s.maxDepth = maxDepth
	s.allowEmptyEnv = allowEmptyEnv

	if fs != nil {
		s.enableFlag = true
		s.flagSet = fs
	}
}

func (s *Options) AddFlags(f *pflag.FlagSet) {
	f.StringSliceVarP(&s.valueFiles, "values", "f", s.valueFiles, "specify values in a YAML file or a URL (can specify multiple)")
	f.StringArrayVar(&s.values, "set", s.values, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringArrayVar(&s.stringValues, "set-string", s.stringValues, "set STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringArrayVar(&s.fileValues, "set-file", s.fileValues, "set values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")
}

func (in *Options) DeepCopy() (out *Options) {
	if in == nil {
		return nil
	}

	out = new(Options)
	*out = *in

	if in.pathsBase != nil {
		in, out := &in.pathsBase, &out.pathsBase
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}

	if in.valueFiles != nil {
		in, out := &in.valueFiles, &out.valueFiles
		*out = make([]string, len(*in))
		copy(*out, *in)
	}

	if in.values != nil {
		in, out := &in.values, &out.values
		*out = make([]string, len(*in))
		copy(*out, *in)
	}

	if in.fileValues != nil {
		in, out := &in.fileValues, &out.fileValues
		*out = make([]string, len(*in))
		copy(*out, *in)
	}

	// skip in.params

	return
}

func (s *Options) ValueFiles() []string {
	return s.valueFiles
}
func (p *Options) Validate() (err error) {
	return nil
}

type Option interface {
	apply(*Options)
}

type funcOption struct {
	f func(*Options)
}

func (p *funcOption) apply(opt *Options) {
	p.f(opt)
}

func newFuncOption(f func(*Options)) *funcOption {
	return &funcOption{
		f: f,
	}
}

// with config object
func WithConfig(path string, config interface{}) Option {
	b, err := yaml.Marshal(config)
	if err != nil {
		panic(err)
	}

	return WithDefaultYaml(path, string(b))
}

// with config yaml
func WithDefaultYaml(path, yamlData string) Option {
	return newFuncOption(func(o *Options) {
		if o.pathsBase == nil {
			o.pathsBase = map[string]string{path: yamlData}
		} else {
			o.pathsBase[path] = yamlData
		}
	})
}

func WithOverrideYaml(path, yamlData string) Option {
	return newFuncOption(func(o *Options) {
		if o.pathsOverride == nil {
			o.pathsOverride = map[string]string{path: yamlData}
		} else {
			o.pathsOverride[path] = yamlData
		}
	})
}

func WithValueFile(valueFiles ...string) Option {
	return newFuncOption(func(o *Options) {
		o.valueFiles = append(o.valueFiles, valueFiles...)
	})
}
