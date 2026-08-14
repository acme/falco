package shared

import (
	"net/url"
	"sort"
	"strings"
)

type QueryString struct {
	Key   string
	Value []string // nil indicates not set in VCL
}

// We implement original querytring struct in order to maname URL queries keeping its order.
// url.Values are useful in Golang but Encode() result does not care its order because it is managed in map
// and always sort by query name. On VCL, we need to keep query raw-order as present, this struct solved them.
type QueryStrings struct {
	Prefix string // protocol, host, port, path
	Items  []*QueryString
}

func ParseQuery(qs string) (*QueryStrings, error) {
	// Find querystring sign
	idx := strings.Index(qs, "?")
	if idx == -1 {
		return &QueryStrings{Prefix: qs}, nil
	}

	ret := &QueryStrings{
		Prefix: qs[0:idx],
	}
	qs = qs[idx+1:]
	for q := range strings.SplitSeq(qs, "&") {
		sp := strings.SplitN(q, "=", 2)
		if len(sp) == 0 {
			continue
		}
		key, err := url.QueryUnescape(sp[0])
		if err != nil {
			return nil, err
		}
		if len(sp) == 1 {
			// e.g ?foo -- equal sign is not preset
			ret.Items = append(ret.Items, &QueryString{Key: key, Value: nil})
			continue
		}
		val, err := url.QueryUnescape(sp[1])
		if err != nil {
			return nil, err
		}
		ret.Add(key, val)
	}
	return ret, nil
}

func (q *QueryStrings) Set(name, val string) {
	for i := range q.Items {
		if q.Items[i].Key != name {
			continue
		}
		q.Items[i].Value = []string{val}
		return
	}

	// set new
	q.Items = append(q.Items, &QueryString{Key: name, Value: []string{val}})
}

func (q *QueryStrings) Add(name, val string) {
	for i := range q.Items {
		if q.Items[i].Key != name {
			continue
		}
		if q.Items[i].Value == nil {
			q.Items[i].Value = []string{}
		}
		q.Items[i].Value = append(q.Items[i].Value, val)
		return
	}

	// append new
	q.Items = append(q.Items, &QueryString{Key: name, Value: []string{val}})
}

func (q *QueryStrings) Get(name string) *string {
	for i := range q.Items {
		if q.Items[i].Key != name {
			continue
		}
		if q.Items[i].Value == nil {
			return nil // nil returns not set string in VCL
		}
		return &q.Items[i].Value[0]
	}
	return nil
}

func (q *QueryStrings) Clean() {
	var cleaned []*QueryString
	for _, v := range q.Items {
		if v.Key == "" {
			continue
		}
		cleaned = append(cleaned, v)
	}
	q.Items = cleaned
}

// Unique keeps only the first parameter for each name, which is how Fastly
// implements the only_unique_keys argument of querystring.sort.
//
// A name that carries a value and the same name with no value at all are
// separate parameters and neither displaces the other, so "b=1&b" keeps both.
// Two valueless copies of a name are duplicates of each other though, so
// "b&b" collapses to one.
func (q *QueryStrings) Unique() {
	seen := make(map[string]struct{})
	var unique []*QueryString
	for _, v := range q.Items {
		if v.Value == nil {
			if _, ok := seen[v.Key]; ok {
				continue
			}
			seen[v.Key] = struct{}{}
		} else if len(v.Value) > 1 {
			v.Value = v.Value[:1]
		}
		unique = append(unique, v)
	}
	q.Items = unique
}

func (q *QueryStrings) Filter(filter func(name string) bool) {
	var filtered []*QueryString
	for _, v := range q.Items {
		if !filter(v.Key) {
			continue
		}
		filtered = append(filtered, v)
	}
	q.Items = filtered
}

type SortMode string

const (
	SortAsc  SortMode = "asc"
	SortDesc SortMode = "desc"
)

func (q *QueryStrings) Sort(mode SortMode) {
	sort.Slice(q.Items, func(i, j int) bool {
		v := q.Items[i].Key > q.Items[j].Key
		if mode == SortAsc {
			return !v
		}
		return v
	})
}

func (q *QueryStrings) String() string {
	var buf strings.Builder
	for i, v := range q.Items {
		key := q.Items[i].Key
		if v.Value == nil {
			buf.WriteString(key)
		} else {
			for j := range v.Value {
				buf.WriteString(key)
				buf.WriteString("=")
				buf.WriteString(url.QueryEscape(v.Value[j]))
				if j != len(v.Value)-1 {
					buf.WriteString("&")
				}
			}
		}
		if i != len(q.Items)-1 {
			buf.WriteString("&")
		}
	}
	var sign string
	if buf.Len() > 0 {
		sign = "?"
	}
	return q.Prefix + sign + buf.String()
}
