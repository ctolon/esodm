package esodm

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

// ReadOptions selects routing, shard preference, realtime behavior and source fields.
// A nil Realtime or Source leaves the server default unchanged.
type ReadOptions struct {
	// Routing must match the indexing route; empty omits routing. Join repositories require an
	// explicit route.
	Routing string
	// Preference is an Elasticsearch shard preference, such as _local or a custom session string;
	// empty uses the server default.
	Preference string
	// Realtime selects realtime GET behavior; nil omits the parameter, false explicitly disables it
	// and true explicitly enables it.
	Realtime *bool
	// Source selects Includes and Excludes field patterns. Nil or empty lists leave source filtering
	// unchanged; exclusions win over inclusions. Source filtering does not change the stored
	// document.
	Source *types.SourceFilter
}

func (o ReadOptions) values() url.Values {
	v := url.Values{}
	if o.Routing != "" {
		v.Set("routing", o.Routing)
	}
	if o.Preference != "" {
		v.Set("preference", o.Preference)
	}
	if o.Realtime != nil {
		v.Set("realtime", strconv.FormatBool(*o.Realtime))
	}
	if o.Source != nil {
		if len(o.Source.Includes) > 0 {
			v.Set("_source_includes", strings.Join(o.Source.Includes, ","))
		}
		if len(o.Source.Excludes) > 0 {
			v.Set("_source_excludes", strings.Join(o.Source.Excludes, ","))
		}
	}
	return v
}
