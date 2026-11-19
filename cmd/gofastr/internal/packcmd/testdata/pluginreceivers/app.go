package pluginreceivers

import "github.com/DonaldMurillo/gofastr/framework"

func register(fwApp *framework.App) {
	fwApp.RegisterPlugin(&adminPlugin{enabled: true})
	fwApp.RegisterPlugin(reportPlugin[int]{})
	fwApp.RegisterPlugin(&exportPlugin[int, string]{})
}
