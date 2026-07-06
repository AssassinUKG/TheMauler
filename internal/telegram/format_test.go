package telegram

import (
	"strings"
	"testing"
)

func TestTelegramHTMLFromMarkdownishEscapesAndFormats(t *testing.T) {
	got := TelegramHTMLFromMarkdownish("**root** `<id>`\n```bash\necho <x>\n```")
	for _, want := range []string{"<b>root</b>", "<code>&lt;id&gt;</code>", "<pre>echo &lt;x&gt;</pre>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestSplitTelegramHTML(t *testing.T) {
	chunks := SplitTelegramHTML(strings.Repeat("a", telegramChunkLimit+100))
	if len(chunks) < 2 {
		t.Fatalf("expected split chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > telegramChunkLimit {
			t.Fatalf("chunk too large: %d", len([]rune(chunk)))
		}
	}
}
