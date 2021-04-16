package resolver

import (
	"net/http"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/database/record"
)

func registerAPI() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "dns/clear",
		Read:        api.PermitUser,
		ActionFunc:  clearNameCache,
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
		Path: `dns/cache`,
		Read: api.PermitUser,
		RecordFunc: func(r *api.Request) (record.Record, error) {
			return recordDatabase.Get(nameRecordsKeyPrefix + r.URL.Query().Get("q"))
		},
		Name:        "Get DNS Record from Cache",
		Description: "Returns cached dns records from the internal cache.",
		Parameters: []api.Parameter{{
			Method:      http.MethodGet,
			Field:       "q",
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
