package resolver

import (
	"net/http"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/database/record"
)

func registerAPI() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "dns/clear",
		Write:       api.PermitUser,
		ActionFunc:  clearNameCacheHandler,
		Name:        "Clear cached DNS records",
		Description: "Deletes all saved DNS records from the database.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "dns/resolvers",
		Read:        api.PermitAnyone,
		StructFunc:  exportDNSResolvers,
		Name:        "List DNS Resolvers",
		Description: "List currently configured DNS resolvers and their status.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path: `dns/cache/{query:[a-z0-9\.-]{0,512}\.[A-Z]{1,32}}`,
		Read: api.PermitUser,
		RecordFunc: func(r *api.Request) (record.Record, error) {
			return recordDatabase.Get(nameRecordsKeyPrefix + r.URLVars["query"])
		},
		Name:        "Get DNS Record from Cache",
		Description: "Returns cached dns records from the internal cache.",
		Parameters: []api.Parameter{{
			Method:      http.MethodGet,
			Field:       "query (in path)",
			Value:       "fqdn and query type",
			Description: "Specify the query like this: `example.com.A`.",
		}},
	}); err != nil {
		return err
	}

	return nil
}

type resolverExport struct {
	*Resolver
	Failing bool
}

func exportDNSResolvers(*api.Request) (interface{}, error) {
	resolversLock.RLock()
	defer resolversLock.RUnlock()

	export := make([]resolverExport, 0, len(globalResolvers))
	for _, r := range globalResolvers {
		export = append(export, resolverExport{
			Resolver: r,
			Failing:  r.Conn.IsFailing(),
		})
	}

	return export, nil
}
