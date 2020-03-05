package marshal

import (
	"errors"
	"strconv"
)

type MarshalledObject struct {
	MajorVersion byte
	MinorVersion byte

	data        []byte
	symbolCache *[]string
	objectCache *[]*MarshalledObject
	size        int
}

type marshalledObjectType byte

var TypeMismatch = errors.New("gorails/marshal: an attempt to implicitly typecast a marshalled object")
var IncompleteData = errors.New("gorails/marshal: incomplete data")

const (
	TypeUnknown marshalledObjectType = 0
	TypeNil     marshalledObjectType = 1
	TypeBool    marshalledObjectType = 2
	TypeInteger marshalledObjectType = 3
	TypeFloat   marshalledObjectType = 4
	TypeString  marshalledObjectType = 5
	TypeArray   marshalledObjectType = 6
	TypeMap     marshalledObjectType = 7
)

// For compatibility
const (
	TYPE_UNKNOWN marshalledObjectType = TypeUnknown
	TYPE_NIL     marshalledObjectType = TypeNil
	TYPE_BOOL    marshalledObjectType = TypeBool
	TYPE_INTEGER marshalledObjectType = TypeInteger
	TYPE_FLOAT   marshalledObjectType = TypeFloat
	TYPE_STRING  marshalledObjectType = TypeString
	TYPE_ARRAY   marshalledObjectType = TypeArray
	TYPE_MAP     marshalledObjectType = TypeMap
)

func newMarshalledObject(majorVersion, minorVersion byte, data []byte, symbolCache *[]string, objectCache *[]*MarshalledObject) *MarshalledObject {
	return newMarshalledObjectWithSize(majorVersion, minorVersion, data, len(data), symbolCache, objectCache)
}

func newMarshalledObjectWithSize(majorVersion, minorVersion byte, data []byte, size int, symbolCache *[]string, objectCache *[]*MarshalledObject) *MarshalledObject {
	return &(MarshalledObject{majorVersion, minorVersion, data, symbolCache, objectCache, size})
}

func CreateMarshalledObject(serializedData []byte) *MarshalledObject {
	var symbolCache []string
	var objectCache []*MarshalledObject
	return newMarshalledObject(serializedData[0], serializedData[1], serializedData[2:], &symbolCache, &objectCache)
}

func (obj *MarshalledObject) GetType() marshalledObjectType {
	if len(obj.data) == 0 {
		return TypeUnknown
	}

	if ref := obj.resolveObjectLink(); ref != nil {
		return ref.GetType()
	}

	switch obj.data[0] {
	case '0':
		return TypeNil
	case 'T', 'F':
		return TypeBool
	case 'i':
		return TypeInteger
	case 'f':
		return TypeFloat
	case ':', ';':
		return TypeString
	case 'I':
		if len(obj.data) > 1 && obj.data[1] == '"' {
			return TypeString
		}
	case '[':
		return TypeArray
	case '{':
		return TypeMap
	}

	return TypeUnknown
}

func (obj *MarshalledObject) GetAsBool() (value bool, err error) {
	err = assertType(obj, TypeBool)
	if err != nil {
		return
	}

	value, _ = parseBool(obj.data)

	return
}

func (obj *MarshalledObject) GetAsInteger() (value int64, err error) {
	err = assertType(obj, TypeInteger)
	if err != nil {
		return
	}

	value, _ = parseInt(obj.data[1:])

	return
}

func (obj *MarshalledObject) GetAsFloat() (value float64, err error) {
	err = assertType(obj, TypeFloat)
	if err != nil {
		return
	}

	str, _ := parseString(obj.data[1:])
	value, err = strconv.ParseFloat(str, 64)

	return
}

func (obj *MarshalledObject) GetAsString() (value string, err error) {
	if ref := obj.resolveObjectLink(); ref != nil {
		return ref.GetAsString()
	}

	err = assertType(obj, TypeString)
	if err != nil {
		return
	}

	obj.cacheObject(obj)

	var cache []string
	if obj.data[0] == ':' {
		value, _ = parseString(obj.data[1:])
		obj.cacheSymbols(value)
	} else if obj.data[0] == ';' {
		refIndex, _ := parseInt(obj.data[1:])
		cache := *(obj.symbolCache)
		value = cache[refIndex]
	} else {
		value, _, cache = parseStringWithEncoding(obj.data[2:])
		obj.cacheSymbols(cache...)
	}

	return
}

func (obj *MarshalledObject) GetAsArray() (value []*MarshalledObject, err error) {
	if ref := obj.resolveObjectLink(); ref != nil {
		return ref.GetAsArray()
	}

	err = assertType(obj, TypeArray)
	if err != nil {
		return
	}

	obj.cacheObject(obj)

	arraySize, offset := parseInt(obj.data[1:])
	offset += 1

	value = make([]*MarshalledObject, arraySize)
	for i := int64(0); i < arraySize; i++ {
		valueSize := newMarshalledObjectWithSize(
			obj.MajorVersion,
			obj.MinorVersion,
			obj.data[offset:],
			0,
			obj.symbolCache,
			obj.objectCache,
		).getSize()

		value[i] = newMarshalledObject(
			obj.MajorVersion,
			obj.MinorVersion,
			obj.data[offset:offset+valueSize],
			obj.symbolCache,
			obj.objectCache,
		)
		obj.cacheObject(value[i])
		offset += valueSize
	}

	obj.size = offset

	return
}

func (obj *MarshalledObject) GetAsMap() (value map[string]*MarshalledObject, err error) {
	if ref := obj.resolveObjectLink(); ref != nil {
		return ref.GetAsMap()
	}

	err = assertType(obj, TypeMap)
	if err != nil {
		return
	}

	obj.cacheObject(obj)

	mapSize, offset := parseInt(obj.data[1:])
	offset += 1

	value = make(map[string]*MarshalledObject, mapSize)
	for i := int64(0); i < mapSize; i++ {
		k := newMarshalledObject(
			obj.MajorVersion,
			obj.MinorVersion,
			obj.data[offset:],
			obj.symbolCache,
			obj.objectCache,
		)
		obj.cacheObject(k)
		offset += k.getSize()

		valueSize := newMarshalledObjectWithSize(
			obj.MajorVersion,
			obj.MinorVersion,
			obj.data[offset:],
			0,
			obj.symbolCache,
			obj.objectCache,
		).getSize()

		v := newMarshalledObject(
			obj.MajorVersion,
			obj.MinorVersion,
			obj.data[offset:offset+valueSize],
			obj.symbolCache,
			obj.objectCache,
		)
		obj.cacheObject(v)
		value[k.ToString()] = v

		offset += valueSize
	}

	obj.size = offset

	return
}

func assertType(obj *MarshalledObject, expectedType marshalledObjectType) (err error) {
	if obj.GetType() != expectedType {
		err = TypeMismatch
	}

	return
}

func (obj *MarshalledObject) getSize() int {
	headerSize, dataSize := 0, 0

	if len(obj.data) > 0 && obj.data[0] == '@' {
		headerSize = 1
		_, dataSize = parseInt(obj.data[1:])
		return headerSize + dataSize
	}

	switch obj.GetType() {
	case TypeNil, TypeBool:
		headerSize = 0
		dataSize = 1
	case TypeInteger:
		headerSize = 1
		_, dataSize = parseInt(obj.data[headerSize:])
	case TypeString, TypeFloat:
		headerSize = 1

		if obj.data[0] == ';' {
			_, dataSize = parseInt(obj.data[headerSize:])
		} else {
			var cache []string

			if obj.data[0] == 'I' {
				headerSize += 1
				_, dataSize, cache = parseStringWithEncoding(obj.data[headerSize:])
				obj.cacheSymbols(cache...)
			} else {
				var symbol string
				symbol, dataSize = parseString(obj.data[headerSize:])
				obj.cacheSymbols(symbol)
			}
		}
	case TypeArray:
		if obj.size == 0 {
			obj.GetAsArray()
		}

		return obj.size
	case TypeMap:
		if obj.size == 0 {
			obj.GetAsMap()
		}

		return obj.size
	}

	return headerSize + dataSize
}

func (obj *MarshalledObject) cacheSymbols(symbols ...string) {
	if len(symbols) == 0 {
		return
	}

	cache := *(obj.symbolCache)

	known := make(map[string]struct{})
	for _, symbol := range cache {
		known[symbol] = struct{}{}
	}

	for _, symbol := range symbols {
		_, exists := known[symbol]

		if !exists {
			cache = append(cache, symbol)
		}
	}

	*(obj.symbolCache) = cache
}

func (obj *MarshalledObject) cacheObject(object *MarshalledObject) {
	if len(object.data) > 0 && (object.data[0] == '@' || object.data[0] == ':' || object.data[0] == ';') {
		return
	}
	if t := obj.GetType(); !(t == TypeString || t == TypeArray || t == TypeMap) {
		return
	}

	cache := *(obj.objectCache)

	for _, o := range cache {
		if object == o {
			return
		}
	}
	cache = append(cache, object)

	*(obj.objectCache) = cache
}

func (obj *MarshalledObject) ToString() (str string) {
	switch obj.GetType() {
	case TypeNil:
		str = "<nil>"
	case TypeBool:
		v, _ := obj.GetAsBool()

		if v {
			str = "true"
		} else {
			str = "false"
		}
	case TypeInteger:
		v, _ := obj.GetAsInteger()
		str = strconv.FormatInt(v, 10)
	case TypeString:
		str, _ = obj.GetAsString()
	case TypeFloat:
		v, _ := obj.GetAsFloat()
		str = strconv.FormatFloat(v, 'f', -1, 64)
	}

	return
}

func (obj *MarshalledObject) resolveObjectLink() *MarshalledObject {
	if len(obj.data) > 0 && obj.data[0] == '@' {
		idx, _ := parseInt(obj.data[1:])
		cache := *(obj.objectCache)

		if int(idx) < len(cache) {
			return cache[idx]
		}
	}

	return nil
}

func parseBool(data []byte) (bool, int) {
	return data[0] == 'T', 1
}

func parseInt(data []byte) (int64, int) {
	if data[0] > 0x05 && data[0] < 0xfb {
		value := int64(data[0])

		if value > 0x7f {
			return -(0xff ^ value + 1) + 5, 1
		} else {
			return value - 5, 1
		}
	} else if data[0] <= 0x05 {
		value := int64(0)
		i := data[0]

		for ; i > 0; i-- {
			value = value<<8 + int64(data[i])
		}

		return value, int(data[0] + 1)
	} else {
		value := int64(0)
		i := 0xff - data[0] + 1

		for ; i > 0; i-- {
			value = value<<8 + (0xff - int64(data[i]))
		}

		return -(value + 1), int(0xff - data[0] + 2)
	}
}

func parseString(data []byte) (string, int) {
	length, headerSize := parseInt(data)
	size := int(length) + headerSize

	return string(data[headerSize:size]), size
}

func parseStringWithEncoding(data []byte) (string, int, []string) {
	cache := make([]string, 0)
	value, size := parseString(data)

	if len(data) > size+1 && (data[size+1] == ':' || data[size+1] == ';') {
		if data[size+1] == ';' {
			_, encSize := parseInt(data[size+2:])
			size += encSize + 1
		} else {
			encSymbol, encSize := parseString(data[size+2:])
			size += encSize + 1
			cache = append(cache, encSymbol)
		}

		if data[size+1] == '"' {
			encoding, encNameSize := parseString(data[size+2:])
			_ = encoding
			size += encNameSize + 1
		} else {
			_, encNameSize := parseBool(data[size+1:])
			size += encNameSize
		}

		size += 1
	}

	return value, size, cache
}
