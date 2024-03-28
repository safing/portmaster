import { ApplicationConfig } from '@angular/core';
import { TauriIntegrationService } from 'src/app/integration/taur-app';

export const appConfig: ApplicationConfig = {
  providers: [
    {
      provide: TauriIntegrationService,
      useClass: TauriIntegrationService,
      deps: []
    },
  ],
};
