package mailbox

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime/quotedprintable"
	"strings"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/charset"
	"golang.org/x/net/html"
)

// Select inline text only. For multipart/alternative use one representation,
// preferring plain text; multipart/mixed can contain several inline parts.
type textPart struct {
	path      []int
	structure *imap.BodyStructure
}

func textParts(b *imap.BodyStructure, path []int) []textPart {
	if strings.EqualFold(b.Disposition, "attachment") || b.Params["name"] != "" || b.DispositionParams["filename"] != "" {
		return nil
	}
	if strings.EqualFold(b.MIMEType, "text") && (strings.EqualFold(b.MIMESubType, "plain") || strings.EqualFold(b.MIMESubType, "html")) {
		if len(path) == 0 {
			path = []int{1}
		}
		return []textPart{{path, b}}
	}
	var parts []textPart
	for i, child := range b.Parts {
		next := append(append([]int(nil), path...), i+1)
		found := textParts(child, next)
		if strings.EqualFold(b.MIMESubType, "alternative") && len(found) > 0 {
			if strings.EqualFold(found[0].structure.MIMESubType, "plain") {
				return found
			}
			parts = found
		} else {
			parts = append(parts, found...)
		}
	}
	return parts
}

func fetchText(c *client.Client, m *imap.Message) (string, error) {
	parts := textParts(m.BodyStructure, nil)
	if len(parts) == 0 {
		return "", nil
	}
	items := []imap.FetchItem{imap.FetchUid}
	var sections []*imap.BodySectionName
	for _, p := range parts {
		section := &imap.BodySectionName{BodyPartName: imap.BodyPartName{Path: p.path}, Peek: true}
		sections = append(sections, section)
		items = append(items, section.FetchItem())
	}
	set := new(imap.SeqSet)
	set.AddNum(m.Uid)
	msgs, err := fetch(c, set, items)
	if err != nil {
		return "", err
	}
	for _, msg := range msgs {
		if msg.Uid != m.Uid || len(msg.Body) == 0 {
			continue
		}
		var texts []string
		for i, section := range sections {
			r := msg.GetBody(section)
			if r == nil {
				return "", fmt.Errorf("missing text part %v", section.Path)
			}
			text, err := decodeText(parts[i].structure, r)
			if err != nil {
				return "", err
			}
			texts = append(texts, text)
		}
		return strings.Join(texts, "\n"), nil
	}
	return "", fmt.Errorf("message changed during fetch; reopen to retry")
}

func decodeText(b *imap.BodyStructure, r io.Reader) (string, error) {
	switch strings.ToLower(b.Encoding) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	}
	if enc := b.Params["charset"]; enc != "" {
		decoded, err := charset.Reader(enc, r)
		if err != nil {
			return "", err
		}
		r = decoded
	}
	if !strings.EqualFold(b.MIMESubType, "html") {
		data, err := io.ReadAll(r)
		return strings.ToValidUTF8(string(data), "�"), err
	}
	z := html.NewTokenizer(r)
	var out strings.Builder
	hiddenDepth := 0
	for {
		token := z.Next()
		switch token {
		case html.ErrorToken:
			if err := z.Err(); err != io.EOF {
				return "", err
			}
			return strings.Join(strings.Fields(out.String()), " "), nil
		case html.StartTagToken, html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style", "head":
				if token == html.StartTagToken {
					hiddenDepth++
				} else if hiddenDepth > 0 {
					hiddenDepth--
				}
			case "br", "div", "p", "tr", "li":
				out.WriteByte(' ')
			}
		case html.TextToken:
			if hiddenDepth == 0 {
				out.Write(z.Text())
			}
		}
	}
}
