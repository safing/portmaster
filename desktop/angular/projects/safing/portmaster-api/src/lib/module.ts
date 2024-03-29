import { ModuleWithProviders, NgModule } from "@angular/core";
import { AppProfileService } from "./app-profile.service";
import { ConfigService } from "./config.service";
import { DebugAPI } from "./debug-api.service";
import { MetaAPI } from "./meta-api.service";
import { Netquery } from "./netquery.service";
import { PortapiService, PORTMASTER_HTTP_API_ENDPOINT, PORTMASTER_WS_API_ENDPOINT } from "./portapi.service";
import { SPNService } from "./spn.service";
import { WebsocketService } from "./websocket.service";

export interface ModuleConfig {
  httpAPI?: string;
  websocketAPI?: string;
}

@NgModule({})
export class PortmasterAPIModule {

  /**
   * Configures a module with additional providers.
   *
   * @param cfg The module configuration defining the Portmaster HTTP and Websocket API endpoints.
   */
  static forRoot(cfg: ModuleConfig = {}): ModuleWithProviders<PortmasterAPIModule> {
    if (cfg.httpAPI === undefined) {
      cfg.httpAPI = `http://${window.location.host}/api`;
    }
    if (cfg.websocketAPI === undefined) {
      cfg.websocketAPI = `ws://${window.location.host}/api/database/v1`;
    }

    return {
      ngModule: PortmasterAPIModule,
      providers: [
        PortapiService,
        WebsocketService,
        MetaAPI,
        ConfigService,
        AppProfileService,
        DebugAPI,
        Netquery,
        SPNService,
        {
          provide: PORTMASTER_HTTP_API_ENDPOINT,
          useValue: cfg.httpAPI,
        },
        {
          provide: PORTMASTER_WS_API_ENDPOINT,
          useValue: cfg.websocketAPI
        }
      ]
    }
  }

}
