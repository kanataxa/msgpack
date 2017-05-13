package msgpack

import (
	"io"
	"math"
	"reflect"
	"strings"

	bufferpool "github.com/lestrrat/go-bufferpool"
	"github.com/pkg/errors"
)

// NewEncoder creates a new Encoder that writes serialized forms
// to the specified io.Writer
//
// Note that Encoders are NEVER meant to be shared concurrently
// between goroutines. You DO NOT write serialized data concurrently
// to the same destination.
func NewEncoder(w io.Writer) *Encoder {
	var dst Writer
	if x, ok := w.(Writer); ok {
		dst = x
	} else {
		dst = NewWriter(w)
	}

	return &Encoder{
		dst: dst,
	}
}

func isExtType(t reflect.Type) (int, bool) {
	muExtEncode.RLock()
	typ, ok := extEncodeRegistry[t]
	muExtEncode.RUnlock()
	if ok {
		return typ, true
	}

	return 0, false
}

var encodeMsgpackerType = reflect.TypeOf((*EncodeMsgpacker)(nil)).Elem()

func isEncodeMsgpacker(t reflect.Type) bool {
	return t.Implements(encodeMsgpackerType)
}

var byteType = reflect.TypeOf(byte(0))

func (e *Encoder) Encode(v interface{}) error {
	switch v := v.(type) {
	case string:
		return e.EncodeString(v)
	case []byte:
		return e.EncodeBytes(v)
	case bool:
		return e.EncodeBool(v)
	case float32:
		return e.EncodeFloat32(v)
	case float64:
		return e.EncodeFloat64(v)
	case uint:
		return e.EncodeUint64(uint64(v))
	case uint8:
		return e.EncodeUint8(v)
	case uint16:
		return e.EncodeUint16(v)
	case uint32:
		return e.EncodeUint32(v)
	case uint64:
		return e.EncodeUint64(v)
	case int:
		return e.EncodeInt64(int64(v))
	case int8:
		return e.EncodeInt8(v)
	case int16:
		return e.EncodeInt16(v)
	case int32:
		return e.EncodeInt32(v)
	case int64:
		return e.EncodeInt64(v)
	}

	// Find the first non-pointer, non-interface{}
	rv := reflect.ValueOf(v)
INDIRECT:
	for {
		if !rv.IsValid() {
			return e.EncodeNil()
		}
		if typ, ok := isExtType(rv.Type()); ok {
			return e.EncodeExt(typ, rv.Interface().(EncodeMsgpackExter))
		}

		if ok := isEncodeMsgpacker(rv.Type()); ok {
			return rv.Interface().(EncodeMsgpacker).EncodeMsgpack(e)
		}
		switch rv.Kind() {
		case reflect.Ptr, reflect.Interface:
			rv = rv.Elem()
		default:
			break INDIRECT
		}
	}

	if !rv.IsValid() {
		return e.EncodeNil()
	}

	v = rv.Interface()
	switch rv.Kind() {
	case reflect.Slice:
		return e.EncodeArray(v)
	case reflect.Map:
		return e.EncodeMap(v)
	case reflect.Struct:
		return e.EncodeStruct(v)
	}

	return errors.Errorf(`msgpack: encode unimplemented for type %s`, rv.Type())
}

func (e *Encoder) EncodeNil() error {
	return e.dst.WriteByte(Nil.Byte())
}

func (e *Encoder) EncodeBool(b bool) error {
	var code Code
	if b {
		code = True
	} else {
		code = False
	}
	return e.dst.WriteByte(code.Byte())
}

func (e *Encoder) EncodeFloat32(f float32) error {
	if err := e.dst.WriteByteUint32(Float.Byte(), math.Float32bits(f)); err != nil {
		return errors.Wrap(err, `msgpack: failed to write Float`)
	}
	return nil
}

func (e *Encoder) EncodeFloat64(f float64) error {
	if err := e.dst.WriteByteUint64(Double.Byte(), math.Float64bits(f)); err != nil {
		return errors.Wrap(err, `msgpack: failed to write Double`)
	}
	return nil
}

func (e *Encoder) EncodeUint8(i uint8) error {
	if err := e.dst.WriteByteUint8(Uint8.Byte(), i); err != nil {
		return errors.Wrap(err, `msgpack: failed to write Uint8`)
	}
	return nil
}

func (e *Encoder) EncodeUint16(i uint16) error {
	if err := e.dst.WriteByteUint16(Uint16.Byte(), i); err != nil {
		return errors.Wrap(err, `msgpack: failed to write Uint16`)
	}
	return nil
}

func (e *Encoder) EncodeUint32(i uint32) error {
	if err := e.dst.WriteByteUint32(Uint32.Byte(), i); err != nil {
		return errors.Wrap(err, `msgpack: failed to write Uint32`)
	}
	return nil
}

func (e *Encoder) EncodeUint64(i uint64) error {
	if err := e.dst.WriteByteUint64(Uint64.Byte(), i); err != nil {
		return errors.Wrap(err, `msgpack: failed to write Uint64`)
	}
	return nil
}

func (e *Encoder) EncodeInt8(i int8) error {
	if err := e.dst.WriteByteUint8(Int8.Byte(), uint8(i)); err != nil {
		return errors.Wrap(err, `msgpack: failed to write Int8`)
	}
	return nil
}

func (e *Encoder) EncodeInt16(i int16) error {
	if err := e.dst.WriteByteUint16(Int16.Byte(), uint16(i)); err != nil {
		return errors.Wrap(err, `msgpack: failed to write Int16`)
	}
	return nil
}

func (e *Encoder) EncodeInt32(i int32) error {
	if err := e.dst.WriteByteUint32(Int32.Byte(), uint32(i)); err != nil {
		return errors.Wrap(err, `msgpack: failed to write Int32`)
	}
	return nil
}

func (e *Encoder) EncodeInt64(i int64) error {
	if err := e.dst.WriteByteUint64(Int64.Byte(), uint64(i)); err != nil {
		return errors.Wrap(err, `msgpack: failed to write Int64`)
	}
	return nil
}

func (e *Encoder) EncodeBytes(b []byte) error {
	l := len(b)

	var w int
	var code Code
	switch {
	case l <= math.MaxUint8:
		code = Bin8
		w = 1
	case l <= math.MaxUint16:
		code = Bin16
		w = 2
	case l <= math.MaxUint32:
		code = Bin32
		w = 4
	default:
		return errors.Errorf(`msgpack: string is too long (len=%d)`, l)
	}

	if err := e.writePreamble(code, w, l); err != nil {
		return errors.Wrap(err, `msgpack: failed to write []byte preamble`)
	}
	e.dst.Write(b)
	return nil
}

func (e *Encoder) EncodeString(s string) error {
	l := len(s)
	switch {
	case l < 32:
		e.dst.WriteByte(FixStr0.Byte() | uint8(l))
	case l <= math.MaxUint8:
		e.dst.WriteByte(Str8.Byte())
		e.dst.WriteUint8(uint8(l))
	case l <= math.MaxUint16:
		e.dst.WriteByte(Str16.Byte())
		e.dst.WriteUint16(uint16(l))
	case l <= math.MaxUint32:
		e.dst.WriteByte(Str32.Byte())
		e.dst.WriteUint32(uint32(l))
	default:
		return errors.Errorf(`msgpack: string is too long (len=%d)`, l)
	}

	e.dst.WriteString(s)
	return nil
}

func (e *Encoder) writePreamble(code Code, w int, l int) error {
	if err := e.dst.WriteByte(code.Byte()); err != nil {
		return errors.Wrap(err, `msgpack: failed to write code`)
	}

	switch w {
	case 1:
		if err := e.dst.WriteUint8(uint8(l)); err != nil {
			return errors.Wrap(err, `msgpack: failed to write length`)
		}
	case 2:
		if err := e.dst.WriteUint16(uint16(l)); err != nil {
			return errors.Wrap(err, `msgpack: failed to write length`)
		}
	case 4:
		if err := e.dst.WriteUint32(uint32(l)); err != nil {
			return errors.Wrap(err, `msgpack: failed to write length`)
		}
	}
	return nil
}

func (e *Encoder) EncodeArrayHeader(l int) error {
	if err := WriteArrayHeader(e.dst, l); err != nil {
		return errors.Wrap(err, `msgpack: failed to write array header`)
	}
	return nil
}

func (e *Encoder) EncodeArray(v interface{}) error {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
	default:
		return errors.Errorf(`msgpack: argument must be an array or a slice`)
	}

	if err := e.EncodeArrayHeader(rv.Len()); err != nil {
		return err
	}

	switch rv.Type().Elem().Kind() {
	case reflect.String:
		return e.encodeArrayString(v)
	case reflect.Bool:
		return e.encodeArrayBool(v)
	case reflect.Int:
		return e.encodeArrayInt(v)
	case reflect.Int8:
		return e.encodeArrayInt8(v)
	case reflect.Int16:
		return e.encodeArrayInt16(v)
	case reflect.Int32:
		return e.encodeArrayInt32(v)
	case reflect.Int64:
		return e.encodeArrayInt64(v)
	case reflect.Uint:
		return e.encodeArrayUint(v)
	case reflect.Uint8:
		return e.encodeArrayUint8(v)
	case reflect.Uint16:
		return e.encodeArrayUint16(v)
	case reflect.Uint32:
		return e.encodeArrayUint32(v)
	case reflect.Uint64:
		return e.encodeArrayUint64(v)
	case reflect.Float32:
		return e.encodeArrayFloat32(v)
	case reflect.Float64:
		return e.encodeArrayFloat64(v)
	}

	for i := 0; i < rv.Len(); i++ {
		if err := e.Encode(rv.Index(i).Interface()); err != nil {
			return errors.Wrap(err, `msgpack: failed to write array payload`)
		}
	}
	return nil
}

func (e *Encoder) EncodeMap(v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map {
		return errors.Errorf(`msgpack: argument to EncodeMap must be a map (not %s)`, rv.Type())
	}
	if rv.Type().Key().Kind() != reflect.String {
		return errors.Errorf(`msgpack: keys to maps must be strings (not %s)`, rv.Type().Key())
	}

	// XXX We do NOT use MapBuilder's convenience methods except for the
	// WriteHeader bit, purely for performance reasons.
	keys := rv.MapKeys()
	WriteMapHeader(e.dst, len(keys))

	// These are silly fast paths for common cases
	switch rv.Type().Elem().Kind() {
	case reflect.String:
		return e.encodeMapString(v)
	case reflect.Bool:
		return e.encodeMapBool(v)
	case reflect.Uint:
		return e.encodeMapUint(v)
	case reflect.Uint8:
		return e.encodeMapUint8(v)
	case reflect.Uint16:
		return e.encodeMapUint16(v)
	case reflect.Uint32:
		return e.encodeMapUint32(v)
	case reflect.Uint64:
		return e.encodeMapUint64(v)
	case reflect.Int:
		return e.encodeMapInt(v)
	case reflect.Int8:
		return e.encodeMapInt8(v)
	case reflect.Int16:
		return e.encodeMapInt16(v)
	case reflect.Int32:
		return e.encodeMapInt32(v)
	case reflect.Int64:
		return e.encodeMapInt64(v)
	case reflect.Float32:
		return e.encodeMapFloat32(v)
	case reflect.Float64:
		return e.encodeMapFloat64(v)
	default:
		for _, key := range keys {
			if err := e.EncodeString(key.Interface().(string)); err != nil {
				return errors.Wrap(err, `failed to encode map key`)
			}

			if err := e.Encode(rv.MapIndex(key).Interface()); err != nil {
				return errors.Wrap(err, `failed to encode map value`)
			}
		}
	}
	return nil
}

func parseMsgpackTag(rv reflect.StructField) (string, bool) {
	var name = rv.Name
	var omitempty bool
	if tag := rv.Tag.Get(`msgpack`); tag != "" {
		l := strings.Split(tag, ",")
		if len(l) > 0 && l[0] != "" {
			name = l[0]
		}

		if len(l) > 1 && l[1] == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

func (e *Encoder) EncodeStruct(v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Struct {
		return errors.Errorf(`msgpack: argument to EncodeStruct must be a struct (not %s)`, rv.Type())
	}
	mapb := NewMapBuilder()

	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		ft := rt.Field(i)
		if ft.PkgPath != "" {
			continue
		}

		name, omitempty := parseMsgpackTag(ft)
		if name == "-" {
			continue
		}

		field := rv.Field(i)
		if omitempty {
			if reflect.DeepEqual(field.Interface(), reflect.Zero(field.Type()).Interface()) {
				continue
			}
		}

		mapb.Add(name, field.Interface())
	}

	if err := mapb.Encode(e.dst); err != nil {
		return errors.Wrap(err, `msgpack: failed to write map payload`)
	}
	return nil
}

func (e *Encoder) EncodeExt(typ int, v EncodeMsgpackExter) error {
	buf := bufferpool.Get()
	defer bufferpool.Release(buf)

	var w = NewWriter(buf)
	if err := v.EncodeMsgpackExt(w); err != nil {
		return errors.Wrapf(err, `msgpack: failed during call to EncodeMsgpackExt for %s`, reflect.TypeOf(v))
	}

	switch l := buf.Len(); {
	case l == 1:
		if err := e.dst.WriteByte(FixExt1.Byte()); err != nil {
			return errors.Wrap(err, `msgpack: failed to write fixext1 code`)
		}
	case l == 2:
		if err := e.dst.WriteByte(FixExt2.Byte()); err != nil {
			return errors.Wrap(err, `msgpack: failed to write fixext2 code`)
		}
	case l == 4:
		if err := e.dst.WriteByte(FixExt4.Byte()); err != nil {
			return errors.Wrap(err, `msgpack: failed to write fixext4 code`)
		}
	case l == 8:
		if err := e.dst.WriteByte(FixExt8.Byte()); err != nil {
			return errors.Wrap(err, `msgpack: failed to write fixext8 code`)
		}
	case l == 16:
		if err := e.dst.WriteByte(FixExt16.Byte()); err != nil {
			return errors.Wrap(err, `msgpack: failed to write fixext16 code`)
		}
	case l <= math.MaxUint8:
		if err := e.dst.WriteByte(Ext8.Byte()); err != nil {
			return errors.Wrap(err, `msgpack: failed to write ext8 code`)
		}
		if err := e.dst.WriteByte(byte(l)); err != nil {
			return errors.Wrap(err, `msgpack: failed to write ext8 payload length`)
		}
	case l <= math.MaxUint16:
		if err := e.dst.WriteByte(Ext16.Byte()); err != nil {
			return errors.Wrap(err, `msgpack: failed to write ext16 code`)
		}
		if err := e.dst.WriteUint16(uint16(l)); err != nil {
			return errors.Wrap(err, `msgpack: failed to write ext16 payload length`)
		}
	case l <= math.MaxUint32:
		if err := e.dst.WriteByte(Ext32.Byte()); err != nil {
			return errors.Wrap(err, `msgpack: failed to write ext32 code`)
		}
		if err := e.dst.WriteUint32(uint32(l)); err != nil {
			return errors.Wrap(err, `msgpack: failed to write ext32 payload length`)
		}
	default:
		return errors.Errorf(`msgpack: extension payload too large: %d bytes`, l)
	}

	if err := e.dst.WriteByte(byte(typ)); err != nil {
		return errors.Wrap(err, `msgpack: failed to write typ code`)
	}

	if _, err := buf.WriteTo(e.dst); err != nil {
		return errors.Wrap(err, `msgpack: failed to write extention payload`)
	}
	return nil
}
