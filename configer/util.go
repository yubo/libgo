package configer

import (
	"fmt"
	"reflect"

	"github.com/spf13/pflag"
)

func SetOptions(allowEnv, allowEmptyEnv bool, maxDepth int, fs *pflag.FlagSet) {
	configerOptions.set(allowEnv, allowEmptyEnv, maxDepth, fs)
}

func AddFlags(f *pflag.FlagSet) {
	configerOptions.addFlags(f)
}

func GetTagOpts(sf reflect.StructField) (tag *TagOpts) {
	return configerOptions.getTagOpts(sf, nil)
}

func ValueFiles() []string {
	return configerOptions.valueFiles
}

// addConfigs: add flags and env from sample's tags
func AddConfigs(fs *pflag.FlagSet, path string, sample interface{}, opts ...Option) error {
	options := configerOptions.deepCopy()
	for _, opt := range opts {
		opt(options)
	}
	options.prefixPath = path

	rv := reflect.Indirect(reflect.ValueOf(sample))
	rt := rv.Type()

	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("Addflag: sample must be a struct, got %v/%v", rv.Kind(), rt)
	}

	return options.addConfigs(parsePath(path), fs, rt)
}

// for testing
func Reset() {
	configerOptions = newOptions()
}

// AddFlagsVar registry var into fs
func AddFlagsVar(fs *pflag.FlagSet, in interface{}) {
	configerOptions.addFlagsVar(fs, in, 0)
}
