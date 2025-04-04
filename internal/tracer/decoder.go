// internal/tracer/decoder.go
package tracer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf16"
)

// DecodingScheme 编码方案
type DecodingScheme int

const (
	// SchemeGSM7 GSM7编码
	SchemeGSM7 DecodingScheme = iota

	// SchemeUTF16 UTF-16编码
	SchemeUTF16

	// SchemeASCII ASCII编码
	SchemeASCII

	// SchemeUnknown 未知编码
	SchemeUnknown
)

// GSM7字符集映射表
var gsm7Charset = map[byte]rune{
	0x00: '@', 0x01: '£', 0x02: '$', 0x03: '¥', 0x04: 'è', 0x05: 'é', 0x06: 'ù', 0x07: 'ì',
	0x08: 'ò', 0x09: 'Ç', 0x0A: '\n', 0x0B: 'Ø', 0x0C: 'ø', 0x0D: '\r', 0x0E: 'Å', 0x0F: 'å',
	0x10: 'Δ', 0x11: '_', 0x12: 'Φ', 0x13: 'Γ', 0x14: 'Λ', 0x15: 'Ω', 0x16: 'Π', 0x17: 'Ψ',
	0x18: 'Σ', 0x19: 'Θ', 0x1A: 'Ξ', 0x1B: 'Æ', 0x1C: 'æ', 0x1D: 'ß', 0x1E: 'É', 0x1F: ' ',
	0x20: ' ', 0x21: '!', 0x22: '"', 0x23: '#', 0x24: '¤', 0x25: '%', 0x26: '&', 0x27: '\'',
	0x28: '(', 0x29: ')', 0x2A: '*', 0x2B: '+', 0x2C: ',', 0x2D: '-', 0x2E: '.', 0x2F: '/',
	0x30: '0', 0x31: '1', 0x32: '2', 0x33: '3', 0x34: '4', 0x35: '5', 0x36: '6', 0x37: '7',
	0x38: '8', 0x39: '9', 0x3A: ':', 0x3B: ';', 0x3C: '<', 0x3D: '=', 0x3E: '>', 0x3F: '?',
	0x40: '¡', 0x41: 'A', 0x42: 'B', 0x43: 'C', 0x44: 'D', 0x45: 'E', 0x46: 'F', 0x47: 'G',
	0x48: 'H', 0x49: 'I', 0x4A: 'J', 0x4B: 'K', 0x4C: 'L', 0x4D: 'M', 0x4E: 'N', 0x4F: 'O',
	0x50: 'P', 0x51: 'Q', 0x52: 'R', 0x53: 'S', 0x54: 'T', 0x55: 'U', 0x56: 'V', 0x57: 'W',
	0x58: 'X', 0x59: 'Y', 0x5A: 'Z', 0x5B: 'Ä', 0x5C: 'Ö', 0x5D: 'Ñ', 0x5E: 'Ü', 0x5F: '§',
	0x60: '¿', 0x61: 'a', 0x62: 'b', 0x63: 'c', 0x64: 'd', 0x65: 'e', 0x66: 'f', 0x67: 'g',
	0x68: 'h', 0x69: 'i', 0x6A: 'j', 0x6B: 'k', 0x6C: 'l', 0x6D: 'm', 0x6E: 'n', 0x6F: 'o',
	0x70: 'p', 0x71: 'q', 0x72: 'r', 0x73: 's', 0x74: 't', 0x75: 'u', 0x76: 'v', 0x77: 'w',
	0x78: 'x', 0x79: 'y', 0x7A: 'z', 0x7B: 'ä', 0x7C: 'ö', 0x7D: 'ñ', 0x7E: 'ü', 0x7F: 'à',
}

// ContentDecoder 短信内容解码器
type ContentDecoder struct{}

// NewContentDecoder 创建新的内容解码器
func NewContentDecoder() *ContentDecoder {
	return &ContentDecoder{}
}

// DetectScheme 检测编码方案
func (d *ContentDecoder) DetectScheme(data []byte) DecodingScheme {
	// 检查是否可能是UTF-16
	if len(data) >= 2 && (data[0] == 0xFF && data[1] == 0xFE || data[0] == 0xFE && data[1] == 0xFF) {
		return SchemeUTF16
	}

	// 检查是否只包含ASCII字符
	isASCII := true
	for _, b := range data {
		if b > 0x7F {
			isASCII = false
			break
		}
	}

	if isASCII {
		return SchemeASCII
	}

	// 默认假设是GSM7编码
	return SchemeGSM7
}

// DecodeContent 解码短信内容
func (d *ContentDecoder) DecodeContent(data []byte, dataCoding byte) string {
	// 根据数据编码选择解码方法
	switch dataCoding {
	case 0x00, 0x01, 0x02, 0x03:
		// GSM7编码
		return d.DecodeGSM7(data)
	case 0x08:
		// UCS2 (UTF-16)编码
		return d.DecodeUTF16(data)
	default:
		// 尝试自动检测
		scheme := d.DetectScheme(data)
		switch scheme {
		case SchemeGSM7:
			return d.DecodeGSM7(data)
		case SchemeUTF16:
			return d.DecodeUTF16(data)
		case SchemeASCII:
			return string(data)
		default:
			// 未知编码，直接返回十六进制表示
			return fmt.Sprintf("HEX:%X", data)
		}
	}
}

// DecodeGSM7 解码GSM7编码内容
func (d *ContentDecoder) DecodeGSM7(data []byte) string {
	var result bytes.Buffer

	for _, b := range data {
		if r, ok := gsm7Charset[b]; ok {
			result.WriteRune(r)
		} else {
			// 未知字符，用问号替代
			result.WriteRune('?')
		}
	}

	return result.String()
}

// DecodeUTF16 解码UTF-16编码内容
func (d *ContentDecoder) DecodeUTF16(data []byte) string {
	if len(data)%2 != 0 {
		return fmt.Sprintf("Invalid UTF-16 data: %X", data)
	}

	// 检测字节序
	bigEndian := true
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
		bigEndian = false
		data = data[2:] // 跳过BOM
	} else if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		bigEndian = true
		data = data[2:] // 跳过BOM
	}

	// 转换为UTF-16代码点
	u16 := make([]uint16, len(data)/2)
	for i := 0; i < len(u16); i++ {
		if bigEndian {
			u16[i] = binary.BigEndian.Uint16(data[i*2:])
		} else {
			u16[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
	}

	// 解码为UTF-8字符串
	return string(utf16.Decode(u16))
}
