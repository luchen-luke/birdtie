package nowcontextquery

import (
	"fmt"
	"strings"
	"unicode"
)

// Terms are bounded literal matching only, not inferred preferences. Common
// request wording is ignored; an empty criterion asks for the selected context.
func Terms(query string) []string {
	q := strings.ToLower(query)
	for _, word := range []string{"帮我找", "帮我", "找一下", "查询", "线上", "在线", "公开", "意图", "伙伴", "有没有", "请", "find", "online", "public", "intent"} {
		q = strings.ReplaceAll(q, word, " ")
	}
	fields := strings.FieldsFunc(q, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) })
	if len(fields) > 6 {
		fields = fields[:6]
	}
	return fields
}
func Matches(query, title string) bool {
	terms := Terms(query)
	if len(terms) == 0 {
		return true
	}
	value := strings.ToLower(title)
	for _, t := range terms {
		if strings.Contains(value, t) {
			return true
		}
	}
	return false
}
func Answer(query string, count int, truncated bool) string {
	if strings.Contains(query, "近一点") || strings.Contains(strings.ToLower(query), "nearer") {
		return "当前查询是线上公开意图，不提供距离排序；不会用地图位置替你推断远近。"
	}
	s := fmt.Sprintf("在你选择的线上情境找到 %d 条当前公开意图。这是公开标题的规则匹配，不代表双方兴趣一致，也没有发送邀请。", count)
	if count == 0 {
		s = "没有找到符合当前文字条件的公开线上意图；未公开、已取消或过期的内容不会显示。"
	}
	if truncated {
		s += "本次最多展示 20 条，请缩小文字条件。"
	}
	return s
}
