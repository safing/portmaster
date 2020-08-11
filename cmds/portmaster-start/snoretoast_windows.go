package main

func init() {
	registerComponent([]Options{
		{
			Name:              "Portmaster SnoreToast Notifier",
			ShortIdentifier:   "notifier-snoretoast", // would otherwise conflict with notifier.
			Identifier:        "notifier/portmaster-snoretoast",
			AllowDownload:     false,
			AllowHidingWindow: true,
			SuppressArgs:      true,
		},
	})
}
