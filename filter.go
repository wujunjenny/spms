// filter.go
package main

import (
	"encoding/json"
	//"fmt"
	"wujun/radixEx"
	"wujun/radixtree"
)

type ContentFilter struct {
	pattern string
	//option 0 , 1
	option int
	arg    string
}

type ContentFilters struct {
	filters   []ContentFilter
	filterfun FilterFunc
}

func (cs *ContentFilters) MarshalJSON() ([]byte, error) {
	tmp := &struct {
		Filters []ContentFilter `json:"filters"`
	}{
		Filters: cs.filters,
	}
	return json.Marshal(tmp)
}

func (c *ContentFilter) MarshalJSON() ([]byte, error) {
	tmp := &struct {
		Pattern string `json:"pattern"`
		//option 0 , 1
		Option int    `json:"option"`
		Arg    string `json:"arg"`
	}{
		Pattern: c.pattern,
		Option:  c.option,
		Arg:     c.arg,
	}

	return json.Marshal(tmp)
}

func (c *ContentFilter) UnmarshalJSON(b []byte) error {
	tmp := &struct {
		Pattern string `json:"pattern"`
		//option 0 , 1
		Option int `json:"option"`
	}{}
	err := json.Unmarshal(b, tmp)

	if err != nil {
		return err
	}

	c.pattern = tmp.Pattern
	c.option = tmp.Option
	return nil
}

var g_filters ContentFilters

type FilterFunc func(inputs ...string) (pass bool, p *ContentFilter)

func (filters *ContentFilters) MakeFilter1() bool {

	tree := radixEx.New()

	for i := range filters.filters {
		key := radixEx.Compile(filters.filters[i].pattern)
		tree.Insert(key, &filters.filters[i])
	}

	filters.filterfun = func(inputs ...string) (pass bool, p *ContentFilter) {
		pass = true
		tree.WalkPath(
			inputs[0],
			func(data interface{}, e *radixEx.Env) bool {
				p = data.(*ContentFilter)
				pass = p.option == 1
				return true
			})

		return
	}
	return true
}

func CompilePattern(Pattern string) (servicecode string, orgaddr string, content string) {

	i := FirstIndex(Pattern, '@')

	if i == -1 {
		return "", "", Pattern
	}

	servicecode = Pattern[:i]

	Pattern = Pattern[i+1:]

	i = FirstIndex(Pattern, '@')

	if i == -1 {
		orgaddr = ""
		content = Pattern
		return
	}

	orgaddr = Pattern[:i]

	content = Pattern[i+1:]

	return
}

func FirstIndex(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

type ref struct {
	_ref interface{}
}

func (filters *ContentFilters) MakeFilter2() bool {

	root := radixtree.New()

	for i := range filters.filters {
		s, d, c := CompilePattern(filters.filters[i].pattern)
		MngLog.Debug("filter ", i, " ", filters.filters[i], " compile:[", s, "][", d, "][", c, "]")
		var dtree *radixtree.Tree
		newdtree := &ref{}
		olddtree, bfind := root.Insert(s, newdtree)
		if bfind {
			dtree = olddtree.(*ref)._ref.(*radixtree.Tree)
		} else {
			dtree = radixtree.New()
		}
		newdtree._ref = dtree

		var ctree *radixEx.Tree //:= radixEx.New()

		refctree := &ref{}
		oldctree, bfind := dtree.Insert(d, refctree)
		if bfind {
			ctree = oldctree.(*ref)._ref.(*radixEx.Tree)
		} else {
			ctree = radixEx.New()
		}
		refctree._ref = ctree
		ctree.Insert(radixEx.Compile(c), &filters.filters[i])

	}

	filters.filterfun = func(inputs ...string) (pass bool, p *ContentFilter) {
		bend := false
		cf := func(data interface{}, e *radixEx.Env) bool {
			p = data.(*ContentFilter)
			pass = p.option == 1
			bend = true
			return true
		}

		cd := func(key string, data interface{}) bool {
			tree := data.(*ref)._ref.(*radixEx.Tree)

			tree.WalkPath(inputs[2], cf)

			return bend
		}

		cs := func(key string, data interface{}) bool {

			tree := data.(*ref)._ref.(*radixtree.Tree)

			tree.WalkPath(inputs[1], cd)

			return bend
		}

		root.WalkPath(inputs[0], cs)

		return
	}
	return true
}

func InitGlobalFilter() {
	conn := DB.GetSPDB()
	defer conn.Close()
	rt, err := LoadSPContents("global", conn)

	if err == nil {
		g_filters.filters = rt
	}

	g_filters.MakeFilter1()

}
