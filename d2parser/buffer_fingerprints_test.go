package d2parser_test

// AST, error, position, and reader-call digests originally captured at 5f01776201890014cbe1131e87e89db891fa5b24.
// The substitutions and eof_dash cases include the corrections tested in token_boundary_test.go.
// Standalone key, map key, and value observations require complete input (fragment_test.go).
var parserBufferLegacyDigests = map[string]string{
	"empty":                "da6530fb9a3c1527e0a09153d468a561fe95573bccb37b482f669b76de3063d8",
	"ordinary":             "468052c03e92e03ff3a4ad7c1099be76926568ceb6b0fd05f084486d2e17d554",
	"raw_escape":           "3c5e7f7938df76769c6fbf98bda865b3eaa0203f42f45762aaf61b8348467671",
	"substitutions":        "bee9743b5d4b0dc1487df6e35f7337c27e2f44c92678f098061623fa6d505628",
	"quoted":               "d6889f6ac8d15991a4943675dca928cb505583457ad0bcb9cd0f4ebbfa5106fd",
	"escaped_newlines":     "7906e96803330b6a18390d11535c5a9c9e33555b97ab91d87d175398f170e465",
	"patterns":             "b1cfd278b01838b5b17f3bd138fc463a17d8d69f320922b49a214530447d958e",
	"edge_groups":          "b8b9ec1f540388ffb17c72f2f04e6f25f67b668c8a2753b4cc0d915abff9c1a7",
	"crlf_unicode":         "a4ad7ae01c7680345243dff8aa22b5124899efd15cc6dee0f5aaf090649fec93",
	"invalid_utf8":         "d0447621a12c8a2fd241de9b4ed90893dc54344525ed93367e7b2dafdc4aa5c8",
	"utf16_bom":            "f9e81b5e2956fb6d0f00e411ff9a79e29c894b9ea7c8782e9a1c128454284be1",
	"short_prefixes":       "b9f27402ceebbdedc1bf58c4fecce98f131b9d53412f9f9da8cf6cba78575638",
	"eof_escape":           "0fa389f628a17c47be0ea1df5d992c89ecc93d66b14c93d272544bec6320254b",
	"eof_dash":             "a21bf98472d41be333e1b354b1c2edb3c6d8249cfe60f7b5837cc25e54aa6067",
	"eof_quote":            "88b45066f353dd3c3b6697f4238b7c777bcc276d4f3095a67c5a20cb17aefefa",
	"comments_blocks":      "1e6b1ef546b96b5610539a9bb1798ad0eafd279cf32b83c9ffd700abd2aee174",
	"long_lookahead":       "65abb11ed91104d93b6db7f66d49182adc6b9d9651d8f60a8fdaca716f52a27c",
	"whitespace_rewind":    "626491e527a15730d0efb035e3353501bcd5bb9c870d8c957ed0caa2ca616a78",
	"delimiters":           "fdff7f5f9da3216b1b79f30653df36438e07707570d8a5c8a87472aa1521232b",
	"reader_error_initial": "569e1dd11f2b25f688662fc6062445e6bf052f0068fec8781a69f36d07984b11",
	"reader_error_prefix":  "9f91836eb59cae16969478390311c9ab8b07dc6a6a9bfcf34dc10b774ca4c0a7",
	"reader_error_escape":  "c717d9044912b6ece4a4230c9fdf7f56b564d0d5830ab87525df1d3fbcbbf111",
}
