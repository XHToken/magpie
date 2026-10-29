package routerhost

import "bytes"

func checkConnectionJournalContract(j *journal, c AdapterContract, input []FilePlan) error {
	err := checkJournalContract(j, c)
	if err == nil {
		return nil
	}
	if j == nil || j.Host != "loomy" || j.Status != "restored" || j.Adapter == nil ||
		!sameContract(c, (LoomyAdapter{}).Contract()) || !sameContract(*j.Adapter, loomyRenamedV3Contract()) ||
		len(j.Files) != len(input) || len(input) != len(c.Files) {
		return err
	}
	for i, f := range j.Files {
		if f.Name != c.Files[i].Name || f.Name != input[i].Name || f.Existed != input[i].Existed ||
			!bytes.Equal(f.Before, input[i].Before) {
			return err
		}
	}
	return nil
}

func loomyRenamedV3Contract() AdapterContract {
	c := loomyV1Contract()
	c.Version = 3
	c.Files[0].Fields = append(c.Files[0].Fields,
		"provider.wefto.npm", "provider.wefto.name", "provider.wefto.options.baseURL",
		"provider.wefto.options.apiKey", "provider.wefto.options.useSessionAuth", "provider.wefto.models",
		"plugin (managed title entry only)")
	c.Files = append(c.Files, FileSpec{Name: loomyTitleFile, Fields: []string{"managed title extension source"}})
	return c
}
