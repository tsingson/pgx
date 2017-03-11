package pgtype

import (
	"encoding/binary"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/jackc/pgx/pgio"
)

const pgTimestamptzHourFormat = "2006-01-02 15:04:05.999999999Z07"
const pgTimestamptzMinuteFormat = "2006-01-02 15:04:05.999999999Z07:00"
const pgTimestamptzSecondFormat = "2006-01-02 15:04:05.999999999Z07:00:00"
const microsecFromUnixEpochToY2K = 946684800 * 1000000

const (
	negativeInfinityMicrosecondOffset = -9223372036854775808
	infinityMicrosecondOffset         = 9223372036854775807
)

type Timestamptz struct {
	Time   time.Time
	Status Status
	InfinityModifier
}

func (dst *Timestamptz) ConvertFrom(src interface{}) error {
	switch value := src.(type) {
	case Timestamptz:
		*dst = value
	case time.Time:
		*dst = Timestamptz{Time: value, Status: Present}
	default:
		if originalSrc, ok := underlyingTimeType(src); ok {
			return dst.ConvertFrom(originalSrc)
		}
		return fmt.Errorf("cannot convert %v to Timestamptz", value)
	}

	return nil
}

func (src *Timestamptz) AssignTo(dst interface{}) error {
	switch v := dst.(type) {
	case *time.Time:
		if src.Status != Present || src.InfinityModifier != None {
			return fmt.Errorf("cannot assign %v to %T", src, dst)
		}
		*v = src.Time
	default:
		if v := reflect.ValueOf(dst); v.Kind() == reflect.Ptr {
			el := v.Elem()
			switch el.Kind() {
			// if dst is a pointer to pointer, strip the pointer and try again
			case reflect.Ptr:
				if src.Status == Null {
					el.Set(reflect.Zero(el.Type()))
					return nil
				}
				if el.IsNil() {
					// allocate destination
					el.Set(reflect.New(el.Type().Elem()))
				}
				return src.AssignTo(el.Interface())
			}
		}
		return fmt.Errorf("cannot assign %v into %T", src, dst)
	}

	return nil
}

func (dst *Timestamptz) DecodeText(src []byte) error {
	if src == nil {
		*dst = Timestamptz{Status: Null}
		return nil
	}

	sbuf := string(src)
	switch sbuf {
	case "infinity":
		*dst = Timestamptz{Status: Present, InfinityModifier: Infinity}
	case "-infinity":
		*dst = Timestamptz{Status: Present, InfinityModifier: -Infinity}
	default:
		var format string
		if sbuf[len(sbuf)-9] == '-' || sbuf[len(sbuf)-9] == '+' {
			format = pgTimestamptzSecondFormat
		} else if sbuf[len(sbuf)-6] == '-' || sbuf[len(sbuf)-6] == '+' {
			format = pgTimestamptzMinuteFormat
		} else {
			format = pgTimestamptzHourFormat
		}

		tim, err := time.Parse(format, sbuf)
		if err != nil {
			return err
		}

		*dst = Timestamptz{Time: tim, Status: Present}
	}

	return nil
}

func (dst *Timestamptz) DecodeBinary(src []byte) error {
	if src == nil {
		*dst = Timestamptz{Status: Null}
		return nil
	}

	if len(src) != 8 {
		return fmt.Errorf("invalid length for timestamptz: %v", len(src))
	}

	microsecSinceY2K := int64(binary.BigEndian.Uint64(src))

	switch microsecSinceY2K {
	case infinityMicrosecondOffset:
		*dst = Timestamptz{Status: Present, InfinityModifier: Infinity}
	case negativeInfinityMicrosecondOffset:
		*dst = Timestamptz{Status: Present, InfinityModifier: -Infinity}
	default:
		microsecSinceUnixEpoch := microsecFromUnixEpochToY2K + microsecSinceY2K
		tim := time.Unix(microsecSinceUnixEpoch/1000000, (microsecSinceUnixEpoch%1000000)*1000)
		*dst = Timestamptz{Time: tim, Status: Present}
	}

	return nil
}

func (src Timestamptz) EncodeText(w io.Writer) (bool, error) {
	switch src.Status {
	case Null:
		return true, nil
	case Undefined:
		return false, errUndefined
	}

	var s string

	switch src.InfinityModifier {
	case None:
		s = src.Time.UTC().Format(pgTimestamptzSecondFormat)
	case Infinity:
		s = "infinity"
	case NegativeInfinity:
		s = "-infinity"
	}

	_, err := io.WriteString(w, s)
	return false, err
}

func (src Timestamptz) EncodeBinary(w io.Writer) (bool, error) {
	switch src.Status {
	case Null:
		return true, nil
	case Undefined:
		return false, errUndefined
	}

	var microsecSinceY2K int64
	switch src.InfinityModifier {
	case None:
		microsecSinceUnixEpoch := src.Time.Unix()*1000000 + int64(src.Time.Nanosecond())/1000
		microsecSinceY2K = microsecSinceUnixEpoch - microsecFromUnixEpochToY2K
	case Infinity:
		microsecSinceY2K = infinityMicrosecondOffset
	case NegativeInfinity:
		microsecSinceY2K = negativeInfinityMicrosecondOffset
	}

	_, err := pgio.WriteInt64(w, microsecSinceY2K)
	return false, err
}