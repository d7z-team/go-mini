package language

import (
	"errors"
	"sort"
	"unicode/utf8"
)

var ErrInvalidPosition = errors.New("invalid UTF-16 document position")

type LineIndex struct {
	text   string
	starts []int
	valid  bool
}

func NewLineIndex(text string) LineIndex {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return LineIndex{text: text, starts: starts, valid: utf8.ValidString(text)}
}

func (i LineIndex) Position(offset int) (Position, error) {
	if offset < 0 || offset > len(i.text) || !i.valid {
		return Position{}, ErrInvalidPosition
	}
	if offset < len(i.text) && i.text[offset]&0xc0 == 0x80 {
		return Position{}, ErrInvalidPosition
	}
	if offset < len(i.text) && i.text[offset] == '\n' && offset > 0 && i.text[offset-1] == '\r' {
		offset--
	}
	line := sort.Search(len(i.starts), func(line int) bool { return i.starts[line] > offset }) - 1
	if line < 0 {
		line = 0
	}
	units := 0
	for _, value := range i.text[i.starts[line]:offset] {
		units++
		if value > 0xffff {
			units++
		}
	}
	return Position{Line: line, Character: units}, nil
}

func (i LineIndex) Offset(position Position) (int, error) {
	if position.Line < 0 || position.Line >= len(i.starts) || position.Character < 0 || !i.valid {
		return 0, ErrInvalidPosition
	}
	start := i.starts[position.Line]
	end := len(i.text)
	if position.Line+1 < len(i.starts) {
		end = i.starts[position.Line+1] - 1
	}
	if position.Line+1 < len(i.starts) && end > start && i.text[end-1] == '\r' {
		end--
	}
	units := 0
	for offset := start; offset < end; {
		if units == position.Character {
			return offset, nil
		}
		value, size := utf8.DecodeRuneInString(i.text[offset:end])
		width := 1
		if value > 0xffff {
			width = 2
		}
		if units+width > position.Character {
			return 0, ErrInvalidPosition
		}
		units += width
		offset += size
	}
	if units == position.Character {
		return end, nil
	}
	return 0, ErrInvalidPosition
}

func (i LineIndex) Range(start, end int) (Range, error) {
	left, err := i.Position(start)
	if err != nil {
		return Range{}, err
	}
	right, err := i.Position(end)
	if err != nil {
		return Range{}, err
	}
	return Range{Start: left, End: right}, nil
}
