package aconfig

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v2"
)

const defaultValueTag = "default"

type Loader struct {
	config LoaderConfig
	fields []*fieldData
}

type LoaderConfig struct {
	UseDefaults bool
	UseFile     bool
	UseEnv      bool
	UseFlag     bool

	EnvPrefix  string
	FlagPrefix string

	Files []string
}

// DefaultConfig ...
func DefaultConfig() LoaderConfig {
	return LoaderConfig{
		UseDefaults: true,
		UseFile:     true,
		UseEnv:      true,
		UseFlag:     true,
	}
}

func NewLoader(config LoaderConfig) *Loader {
	if config.EnvPrefix != "" {
		config.EnvPrefix += "_"
	}
	if config.FlagPrefix != "" {
		config.FlagPrefix += "."
	}
	return &Loader{config: config}
}

func (l *Loader) Load(into interface{}) error {
	l.fields = getFields(into)

	if l.config.UseDefaults {
		if err := l.loadDefaults(); err != nil {
			return err
		}
	}
	if l.config.UseFile {
		if err := l.loadFromFile(into); err != nil {
			return err
		}
	}
	if l.config.UseEnv {
		if err := l.loadEnvironment(); err != nil {
			return err
		}
	}
	if l.config.UseFlag {
		if err := l.loadFlags(); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loader) loadDefaults() error {
	for _, fd := range l.fields {
		if err := l.setFieldData(fd, fd.DefaultValue); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loader) loadFromFile(dst interface{}) error {
	for _, file := range l.config.Files {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()

		ext := strings.ToLower(filepath.Ext(file))
		switch ext {
		case ".yaml", ".yml":
			err = yaml.NewDecoder(f).Decode(dst)
		case ".json":
			err = json.NewDecoder(f).Decode(dst)
		case ".toml":
			_, err = toml.DecodeReader(f, dst)
		default:
			return fmt.Errorf("aconfig: file format '%q' isn't supported", ext)
		}
		if err != nil {
			return fmt.Errorf("aconfig: file parsing error: %s", err.Error())
		}
		break
	}
	return nil
}

func (l *Loader) loadEnvironment() error {
	for _, field := range l.fields {
		envName := l.getEnvName(field.FullName())
		v, ok := os.LookupEnv(envName)
		if !ok {
			continue
		}
		if err := l.setFieldData(field, v); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loader) loadFlags() error {
	if !flag.Parsed() {
		flag.Parse()
	}

	for _, field := range l.fields {
		flagName := l.getFlagName(field.FullName())
		flg := flag.Lookup(flagName)
		if flg == nil {
			continue
		}
		if err := l.setFieldData(field, flg.Value.String()); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loader) getEnvName(name string) string {
	return strings.ToUpper(l.config.EnvPrefix + strings.ReplaceAll(name, ".", "_"))
}

func (l *Loader) getFlagName(name string) string {
	return strings.ToLower(l.config.FlagPrefix + name)
}

func (l *Loader) setFieldData(field *fieldData, value string) error {
	setter, ok := settersByKind[field.Value.Kind()]
	if ok {
		return setter(field, value)
	}
	panic(fmt.Sprintf("unknown kind: %#v %#v", field.Value.Kind(), field))
	return nil
}

func getFields(x interface{}) []*fieldData {
	// TODO: check not struct
	valueObject := reflect.ValueOf(x).Elem()
	return getFieldsHelper(valueObject, nil)
}

func getFieldsHelper(valueObject reflect.Value, parent *fieldData) []*fieldData {
	typeObject := valueObject.Type()
	count := valueObject.NumField()

	fields := make([]*fieldData, 0, count)
	for i := 0; i < count; i++ {
		value := valueObject.Field(i)
		field := typeObject.Field(i)

		if !value.CanSet() {
			continue
		}

		// TODO: pointers

		fd := &fieldData{
			Name:         field.Name,
			Parent:       parent,
			Value:        value,
			Field:        field,
			DefaultValue: field.Tag.Get(defaultValueTag),
		}

		// if just a field - add and process next, else expand struct
		if field.Type.Kind() != reflect.Struct {
			fields = append(fields, fd)
		} else {
			parent := fd
			// remove prefix fpr embedded struct
			if field.Anonymous {
				parent = fd.Parent
			}
			fields = append(fields, getFieldsHelper(value, parent)...)
		}
	}
	return fields
}

type fieldData struct {
	Parent       *fieldData
	Name         string
	Field        reflect.StructField
	Value        reflect.Value
	DefaultValue string
}

func (f *fieldData) FullName() string {
	switch {
	case f == nil:
		return ""
	case f.Parent == nil:
		return f.Name
	default:
		return f.Parent.FullName() + "." + f.Name
	}
}

type setterKindFn func(field *fieldData, value string) error

var settersByKind = map[reflect.Kind]setterKindFn{
	reflect.Bool:   setBool,
	reflect.String: setString,

	reflect.Int:   setInt,
	reflect.Int8:  setInt,
	reflect.Int16: setInt,
	reflect.Int32: setInt,
	reflect.Int64: setInt64,

	reflect.Uint:   setUint,
	reflect.Uint8:  setUint,
	reflect.Uint16: setUint,
	reflect.Uint32: setUint,
	reflect.Uint64: setUint,

	reflect.Float32: setFloat,
	reflect.Float64: setFloat,
}

func setBool(field *fieldData, value string) error {
	val, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	field.Value.SetBool(val)
	return nil
}

func setInt(field *fieldData, value string) error {
	val, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return err
	}
	field.Value.SetInt(val)
	return nil
}

func setInt64(field *fieldData, value string) error {
	if field.Field.Type == reflect.TypeOf(time.Second) {
		val, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		field.Value.Set(reflect.ValueOf(val))
		return nil
	} else {
		return setInt(field, value)
	}
}

func setUint(field *fieldData, value string) error {
	val, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return err
	}
	field.Value.SetUint(val)
	return nil
}

func setFloat(field *fieldData, value string) error {
	val, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return err
	}
	field.Value.SetFloat(val)
	return nil
}

func setString(field *fieldData, value string) error {
	field.Value.SetString(value)
	return nil
}