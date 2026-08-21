package nodes

import (
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

type countryHint struct {
	code    string
	name    string
	flag    string
	aliases []string
}

var (
	genericNodeName = regexp.MustCompile(`(?i)^(node|imported node)(\s+\d+)?$`)
	countryHints    = []countryHint{
		{code: "AM", name: "Armenia", flag: "🇦🇲", aliases: []string{"armenia", "arm"}},
		{code: "AT", name: "Austria", flag: "🇦🇹", aliases: []string{"austria", "aut"}},
		{code: "BG", name: "Bulgaria", flag: "🇧🇬", aliases: []string{"bulgaria", "bgr"}},
		{code: "CA", name: "Canada", flag: "🇨🇦", aliases: []string{"canada", "can"}},
		{code: "CZ", name: "Czechia", flag: "🇨🇿", aliases: []string{"czechia", "czech", "cze"}},
		{code: "DE", name: "Germany", flag: "🇩🇪", aliases: []string{"germany", "deu", "ger"}},
		{code: "EE", name: "Estonia", flag: "🇪🇪", aliases: []string{"estonia", "est"}},
		{code: "ES", name: "Spain", flag: "🇪🇸", aliases: []string{"spain", "esp"}},
		{code: "FI", name: "Finland", flag: "🇫🇮", aliases: []string{"finland", "fin"}},
		{code: "FR", name: "France", flag: "🇫🇷", aliases: []string{"france", "fra"}},
		{code: "GB", name: "Britain", flag: "🇬🇧", aliases: []string{"britain", "united kingdom", "gbr", "uk"}},
		{code: "IL", name: "Israel", flag: "🇮🇱", aliases: []string{"israel", "isr"}},
		{code: "IN", name: "India", flag: "🇮🇳", aliases: []string{"india", "ind"}},
		{code: "KZ", name: "Kazakhstan", flag: "🇰🇿", aliases: []string{"kazakhstan", "kaz"}},
		{code: "LV", name: "Latvia", flag: "🇱🇻", aliases: []string{"latvia", "lva"}},
		{code: "NL", name: "Netherlands", flag: "🇳🇱", aliases: []string{"netherlands", "nld", "nl"}},
		{code: "PL", name: "Poland", flag: "🇵🇱", aliases: []string{"poland", "pol", "pl"}},
		{code: "SE", name: "Sweden", flag: "🇸🇪", aliases: []string{"sweden", "swe"}},
		{code: "SG", name: "Singapore", flag: "🇸🇬", aliases: []string{"singapore", "sgp"}},
		{code: "TH", name: "Thailand", flag: "🇹🇭", aliases: []string{"thailand", "tha"}},
		{code: "TR", name: "Turkey", flag: "🇹🇷", aliases: []string{"turkey", "tur"}},
		{code: "AE", name: "UAE", flag: "🇦🇪", aliases: []string{"uae", "united arab emirates", "are"}},
		{code: "US", name: "USA", flag: "🇺🇸", aliases: []string{"usa", "united states", "us"}},
		{code: "UZ", name: "Uzbekistan", flag: "🇺🇿", aliases: []string{"uzbekistan", "uzb"}},
	}
)

func nodeDisplayName(name, host string) (string, string) {
	name = safeName(name, "Imported node")
	hint, ok := inferCountry(name, host)
	if genericNodeName.MatchString(name) {
		label := shortHostLabel(host)
		if ok {
			return strings.TrimSpace(hint.flag + " " + hint.name + " · " + label), hint.code
		}
		return label, ""
	}
	if ok && !containsFlag(name) {
		return hint.flag + " " + name, hint.code
	}
	if ok {
		return name, hint.code
	}
	return name, ""
}

func displayAddress(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func inferCountry(name, host string) (countryHint, bool) {
	nameWords := hintWords(name)
	hostWords := hintWords(host)
	for _, hint := range countryHints {
		for _, alias := range hint.aliases {
			if nameWords[alias] || hostWords[alias] {
				return hint, true
			}
		}
	}
	return countryHint{}, false
}

func hintWords(value string) map[string]bool {
	result := make(map[string]bool)
	for _, item := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	}) {
		result[item] = true
	}
	return result
}

func containsFlag(value string) bool {
	count := 0
	for _, r := range value {
		if r >= 0x1F1E6 && r <= 0x1F1FF {
			count++
			if count >= 2 {
				return true
			}
		} else {
			count = 0
		}
	}
	return false
}

func shortHostLabel(host string) string {
	host = strings.TrimSpace(host)
	if parsed := net.ParseIP(host); parsed != nil {
		return parsed.String()
	}
	if first, _, ok := strings.Cut(host, "."); ok && first != "" {
		return first
	}
	return host
}
