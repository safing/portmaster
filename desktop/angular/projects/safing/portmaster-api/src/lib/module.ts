import { ModuleWithProviders, NgModule } from "@angular/core";
import { AppProfileService } from "./app-profile.service";
import { ConfigService } from "./config.service";
import { DebugAPI } from "./debug-api.service";
import { MetaAPI } from "./meta-api.service";
import { Netquery } from "./netquery.service";
import { PortapiService, PORTMASTER_HTTP_API_ENDPOINT, PORTMASTER_WS_API_ENDPOINT } from "./portapi.service";
import { SPNService } from "./spn.service";
import { WebsocketService } from "./websocket.service";
import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { TauriHttpInterceptor } from "./tauri-http-interceptor";

export interface ModuleConfig {
  httpAPI?: string;
  websocketAPI?: string;
}

// Simple function to detect if the app is running in a Tauri environment
export function IsTauriEnvironment(): boolean {
  return '__TAURI__' in window;
}

// Factory function to provide the appropriate HTTP client configuration
//
// This function determines the appropriate HTTP client configuration based on the runtime environment.
// If the application is running in a Tauri environment, it uses the TauriHttpInterceptor to ensure
// that all HTTP requests are made from the application binary instead of the WebView instance.
// This allows for more direct and controlled communication with the Portmaster API.
// In other environments (e.g., browser, Electron), the standard HttpClient is used without any interceptors.
export function HttpClientProviderFactory() {
  if (IsTauriEnvironment()) 
  {
    console.log("[app] running under tauri - using TauriHttpClient");
    return provideHttpClient(
      withInterceptors([TauriHttpInterceptor])
    );
  } 
  else 
  {
    console.log("[app] running in browser - using default HttpClient");
    return provideHttpClient();
  }
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
        HttpClientProviderFactory(), 
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
