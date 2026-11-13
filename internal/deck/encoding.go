package deck

import (
	"strconv"
	"strings"
)

// winAnsi is the WinAnsiEncoding (Windows-1252) table, also used as the
// default for simple fonts without an explicit encoding.
var winAnsi = func() [256]rune {
	var t [256]rune
	for i := 0x20; i < 0x7f; i++ {
		t[i] = rune(i)
	}
	for i := 0xa0; i <= 0xff; i++ {
		t[i] = rune(i)
	}
	cp := map[int]rune{0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†', 0x87: '‡', 0x88: 'ˆ', 0x89: '‰', 0x8a: 'Š', 0x8b: '‹', 0x8c: 'Œ', 0x8e: 'Ž',
		0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—', 0x98: '˜', 0x99: '™', 0x9a: 'š', 0x9b: '›', 0x9c: 'œ', 0x9e: 'ž', 0x9f: 'Ÿ'}
	for k, v := range cp {
		t[k] = v
	}
	t[0xad] = '-'
	return t
}()

const macRomanHigh = "ÄÅÇÉÑÖÜáàâäãåçéèêëíìîïñóòôöõúùûü†°¢£§•¶ß®©™´¨≠ÆØ∞±≤≥¥µ∂∑∏π∫ªºΩæø¿¡¬√ƒ≈∆«»… ÀÃÕŒœ–—“”‘’÷◊ÿŸ⁄€‹›ﬁﬂ‡·‚„‰ÂÊÁËÈÍÎÏÌÓÔÒÚÛÙıˆ˜¯˘˙˚¸˝˛ˇ"

func macRoman() [256]rune {
	var t [256]rune
	for i := 0x20; i < 0x7f; i++ {
		t[i] = rune(i)
	}
	i := 0x80
	for _, r := range macRomanHigh {
		if i > 0xff {
			break
		}
		t[i] = r
		i++
	}
	return t
}

// glyphNames is a subset of the Adobe Glyph List covering text commonly
// found in decks. Single-letter names map to themselves; uniXXXX and uXXXX
// names are decoded numerically.
var glyphNames = map[string]rune{
	"space": ' ', "exclam": '!', "quotedbl": '"', "numbersign": '#', "dollar": '$', "percent": '%', "ampersand": '&',
	"quotesingle": '\'', "parenleft": '(', "parenright": ')', "asterisk": '*', "plus": '+', "comma": ',', "hyphen": '-',
	"period": '.', "slash": '/', "zero": '0', "one": '1', "two": '2', "three": '3', "four": '4', "five": '5', "six": '6',
	"seven": '7', "eight": '8', "nine": '9', "colon": ':', "semicolon": ';', "less": '<', "equal": '=', "greater": '>',
	"question": '?', "at": '@', "bracketleft": '[', "backslash": '\\', "bracketright": ']', "asciicircum": '^',
	"underscore": '_', "grave": '`', "braceleft": '{', "bar": '|', "braceright": '}', "asciitilde": '~',
	"quoteleft": '‘', "quoteright": '’', "quotedblleft": '“', "quotedblright": '”', "quotesinglbase": '‚', "quotedblbase": '„',
	"bullet": '•', "endash": '–', "emdash": '—', "ellipsis": '…', "trademark": '™', "copyright": '©', "registered": '®',
	"degree": '°', "dagger": '†', "daggerdbl": '‡', "sterling": '£', "Euro": '€', "euro": '€', "yen": '¥', "cent": '¢',
	"section": '§', "paragraph": '¶', "periodcentered": '·', "multiply": '×', "divide": '÷', "minus": '−', "plusminus": '±',
	"guillemotleft": '«', "guillemotright": '»', "guilsinglleft": '‹', "guilsinglright": '›', "exclamdown": '¡', "questiondown": '¿',
	"fi": 'ﬁ', "fl": 'ﬂ', "ff": 'ﬀ', "ffi": 'ﬃ', "ffl": 'ﬄ', "nbspace": ' ', "uni00A0": ' ', "dotlessi": 'ı',
	"Aacute": 'Á', "aacute": 'á', "Agrave": 'À', "agrave": 'à', "Acircumflex": 'Â', "acircumflex": 'â', "Adieresis": 'Ä', "adieresis": 'ä',
	"Atilde": 'Ã', "atilde": 'ã', "Aring": 'Å', "aring": 'å', "AE": 'Æ', "ae": 'æ', "Ccedilla": 'Ç', "ccedilla": 'ç',
	"Eacute": 'É', "eacute": 'é', "Egrave": 'È', "egrave": 'è', "Ecircumflex": 'Ê', "ecircumflex": 'ê', "Edieresis": 'Ë', "edieresis": 'ë',
	"Iacute": 'Í', "iacute": 'í', "Igrave": 'Ì', "igrave": 'ì', "Icircumflex": 'Î', "icircumflex": 'î', "Idieresis": 'Ï', "idieresis": 'ï',
	"Ntilde": 'Ñ', "ntilde": 'ñ', "Oacute": 'Ó', "oacute": 'ó', "Ograve": 'Ò', "ograve": 'ò', "Ocircumflex": 'Ô', "ocircumflex": 'ô',
	"Odieresis": 'Ö', "odieresis": 'ö', "Otilde": 'Õ', "otilde": 'õ', "Oslash": 'Ø', "oslash": 'ø', "OE": 'Œ', "oe": 'œ',
	"Uacute": 'Ú', "uacute": 'ú', "Ugrave": 'Ù', "ugrave": 'ù', "Ucircumflex": 'Û', "ucircumflex": 'û', "Udieresis": 'Ü', "udieresis": 'ü',
	"Yacute": 'Ý', "yacute": 'ý', "ydieresis": 'ÿ', "Ydieresis": 'Ÿ', "germandbls": 'ß', "Scaron": 'Š', "scaron": 'š', "Zcaron": 'Ž', "zcaron": 'ž',
	"arrowright": '→', "arrowleft": '←', "checkmark": '✓',
}

// glyphRune maps a glyph name to a rune, or 0 if unknown.
func glyphRune(name string) rune {
	if i := strings.IndexByte(name, '.'); i > 0 { // "a.sc", "one.oldstyle"
		name = name[:i]
	}
	if r, ok := glyphNames[name]; ok {
		return r
	}
	if len(name) == 1 && name[0] >= 0x20 && name[0] < 0x7f {
		return rune(name[0])
	}
	for _, p := range []string{"uni", "u"} {
		if strings.HasPrefix(name, p) && len(name) >= len(p)+4 {
			hexPart := name[len(p):]
			if p == "uni" {
				hexPart = hexPart[:4]
			}
			if v, err := strconv.ParseUint(hexPart, 16, 32); err == nil && v > 0x1f && v < 0x110000 {
				return rune(v)
			}
		}
	}
	return 0
}
