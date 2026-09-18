package regexplib

import (
	"regexp"
)

type RegexpHost struct{}

func (h *RegexpHost) Match(pattern string, b []byte) (bool, error) {
	return regexp.Match(pattern, b)
}

func (h *RegexpHost) MatchString(pattern, s string) (bool, error) {
	return regexp.MatchString(pattern, s)
}

func (h *RegexpHost) QuoteMeta(s string) string {
	return regexp.QuoteMeta(s)
}

func (h *RegexpHost) FindString(pattern, s string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	return re.FindString(s)
}

func (h *RegexpHost) FindAllString(pattern, s string, n int) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return re.FindAllString(s, n), nil
}

func (h *RegexpHost) FindStringIndex(pattern, s string) ([]int, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return re.FindStringIndex(s), nil
}

func (h *RegexpHost) FindStringSubmatch(pattern, s string) []string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	return re.FindStringSubmatch(s)
}

func (h *RegexpHost) FindStringSubmatchIndex(pattern, s string) ([]int, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return re.FindStringSubmatchIndex(s), nil
}

func (h *RegexpHost) FindAllStringSubmatch(pattern, s string, n int) ([][]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return re.FindAllStringSubmatch(s, n), nil
}

func (h *RegexpHost) ReplaceAllString(pattern, src, repl string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(src, repl), nil
}

func (h *RegexpHost) ReplaceAllLiteralString(pattern, src, repl string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllLiteralString(src, repl), nil
}

func (h *RegexpHost) Split(pattern, s string, n int) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return re.Split(s, n), nil
}
