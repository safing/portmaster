package firewall

import (
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/core"
	"github.com/safing/portmaster/spn/captain"
)

// Configuration Keys.
var (
	CfgOptionEnableFilterKey = "filter/enable"
	filterEnabled            config.BoolOption

	CfgOptionAskWithSystemNotificationsKey   = "filter/askWithSystemNotifications"
	cfgOptionAskWithSystemNotificationsOrder = 2
	askWithSystemNotifications               config.BoolOption

	CfgOptionAskTimeoutKey   = "filter/askTimeout"
	cfgOptionAskTimeoutOrder = 3
	askTimeout               config.IntOption

	CfgOptionPermanentVerdictsKey   = "filter/permanentVerdicts"
	cfgOptionPermanentVerdictsOrder = 80
	permanentVerdicts               config.BoolOption

	CfgOptionDNSQueryInterceptionKey   = "filter/dnsQueryInterception"
	cfgOptionDNSQueryInterceptionOrder = 81
	dnsQueryInterception               config.BoolOption
)

func registerConfig() error {
	err := config.Register(&config.Option{
		Name:           "Включить Фильтр Приватности",
		Key:            CfgOptionEnableFilterKey,
		Description:    "Включить фильтр приватности. При отключении все защитные функции фильтра полностью отключаются на этом устройстве. Не предназначено для отключения в продакшене - отключайте только для тестирования.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.CategoryAnnotation: "Основные",
		},
	})
	if err != nil {
		return err
	}
	filterEnabled = config.Concurrent.GetAsBool(CfgOptionEnableFilterKey, true)

	err = config.Register(&config.Option{
		Name:           "Постоянные Решения",
		Key:            CfgOptionPermanentVerdictsKey,
		Description:    "Системная интеграция Portmaster перехватывает каждый пакет. Обычно первого пакета достаточно для принятия решения о соединении - разрешить или запретить. Постоянные решения означают, что Portmaster сообщит системе, что больше не хочет видеть пакеты этого соединения. Это значительно повышает производительность.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionPermanentVerdictsOrder,
			config.CategoryAnnotation:     "Расширенные",
		},
	})
	if err != nil {
		return err
	}
	permanentVerdicts = config.Concurrent.GetAsBool(CfgOptionPermanentVerdictsKey, true)

	err = config.Register(&config.Option{
		Name:           "Бесшовная DNS Интеграция",
		Key:            CfgOptionDNSQueryInterceptionKey,
		Description:    "Перехватывать и перенаправлять DNS-запросы на внутренний DNS-сервер Portmaster. Это обеспечивает бесшовную интеграцию DNS без необходимости настройки системы или другого ПО. Однако это может вызвать проблемы совместимости с другими программами, которые делают то же самое.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionDNSQueryInterceptionOrder,
			config.CategoryAnnotation:     "Расширенные",
		},
	})
	if err != nil {
		return err
	}
	dnsQueryInterception = config.Concurrent.GetAsBool(CfgOptionDNSQueryInterceptionKey, true)

	err = config.Register(&config.Option{
		Name:           "Уведомления на Рабочий Стол",
		Key:            CfgOptionAskWithSystemNotificationsKey,
		Description:    `Помимо показа уведомлений в приложении Portmaster, также отправлять их на рабочий стол. Требуется запущенный Portmaster Notifier. Должны быть включены уведомления на рабочий стол.`,
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		DefaultValue:   true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionAskWithSystemNotificationsOrder,
			config.CategoryAnnotation:     "Основные",
			config.RequiresAnnotation: config.ValueRequirement{
				Key:   notifications.CfgUseSystemNotificationsKey,
				Value: true,
			},
		},
	})
	if err != nil {
		return err
	}
	askWithSystemNotifications = config.Concurrent.GetAsBool(CfgOptionAskWithSystemNotificationsKey, true)

	err = config.Register(&config.Option{
		Name:           "Таймаут Уведомлений",
		Key:            CfgOptionAskTimeoutKey,
		Description:    "Как долго Portmaster будет ждать ответа на уведомление. Обратите внимание, что уведомления на рабочем столе могут не учитывать это или иметь собственные ограничения.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		DefaultValue:   60,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionAskTimeoutOrder,
			config.UnitAnnotation:         "секунд",
			config.CategoryAnnotation:     "Основные",
		},
		ValidationRegex: `^[1-9][0-9]{1,5}$`,
	})
	if err != nil {
		return err
	}
	askTimeout = config.Concurrent.GetAsInt(CfgOptionAskTimeoutKey, 60)

	return nil
}

// Config variables for interception and filter module.
// Everything is registered by the interception module, as the filter module
// can be disabled.
var (
	devMode          config.BoolOption
	apiListenAddress config.StringOption

	tunnelEnabled     config.BoolOption
	useCommunityNodes config.BoolOption

	configReady = abool.New()
)

func getConfig() {
	devMode = config.Concurrent.GetAsBool(core.CfgDevModeKey, false)
	apiListenAddress = config.GetAsString(api.CfgDefaultListenAddressKey, "")

	tunnelEnabled = config.Concurrent.GetAsBool(captain.CfgOptionEnableSPNKey, false)
	useCommunityNodes = config.Concurrent.GetAsBool(captain.CfgOptionUseCommunityNodesKey, true)

	configReady.Set()
}
