import { enableProdMode, importProvidersFrom } from '@angular/core';
import { platformBrowserDynamic } from '@angular/platform-browser-dynamic';

import { AppModule } from './app/app.module';
import { environment } from './environments/environment';
import { INTEGRATION_SERVICE, integrationServiceFactory } from './app/integration';
import { bootstrapApplication } from '@angular/platform-browser';
import { PromptWidgetComponent } from './app/shared/prompt-list';
import { PromptEntryPointComponent } from './app/prompt-entrypoint/prompt-entrypoint';
import { provideHttpClient } from '@angular/common/http';
import { provideRouter } from '@angular/router';
import { PortmasterAPIModule } from '@safing/portmaster-api';
import { NotificationsService } from './app/services';
import { TauriIntegrationService } from './app/integration/taur-app';

if (environment.production) {
  enableProdMode();
}

if (typeof (CSS as any)['registerProperty'] === 'function') {
  (CSS as any).registerProperty({
    name: '--lock-color',
    syntax: '*',
    inherits: true,
    initialValue: '10, 10, 10'
  })
}

function handleExternalResources(e: Event) {
  // TODO: 
  //    This code executes "openExternal()" when any "<a />" element in the app is clicked.
  //    This could potentially be a security issue.
  //    We should consider restricting this to only external links that belong to a certain domain (e.g., https://safing.io).
  
  // get click target
  let target: HTMLElement | null = e.target as HTMLElement;
    
  // traverse until we reach element "<a />"
  while (!!target && target.tagName !== "A") {
    target = target.parentElement;
  }

  if (!!target) {
    let href = target.getAttribute("href");
    if (href?.startsWith("blob")) {
      return
    }

    if (!!href && !href.includes(location.hostname)) {
      e.preventDefault();

      integrationServiceFactory().openExternal(href);
    }
  }
}

if (document.addEventListener) {
  document.addEventListener("click", handleExternalResources);
}

// load the font file but make sure to use the slimfix version
// windows.
{
  // we cannot use document.writeXX here as it's not allowed to
  // write to Document from an async loaded script.

  let linkTag = document.createElement("link");
  linkTag.rel = "stylesheet";
  linkTag.href = "/assets/fonts/roboto.css";
  if (navigator.platform.startsWith("Win")) {
    linkTag.href = "/assets/fonts/roboto-slimfix.css"
  }

  document.head.appendChild(linkTag);
}


if (location.pathname !== "/prompt") {
  // bootstrap our normal application
  platformBrowserDynamic().bootstrapModule(AppModule)
    .catch(err => console.error(err));

} else {
  // bootstrap the prompt interface
  bootstrapApplication(PromptEntryPointComponent, {
    providers: [
      provideHttpClient(),
      importProvidersFrom(PortmasterAPIModule.forRoot({
        websocketAPI: "ws://localhost:817/api/database/v1",
        httpAPI: "http://localhost:817/api"
      })),
      NotificationsService,
      {
        provide: INTEGRATION_SERVICE,
        useClass: TauriIntegrationService
      }
    ],
  })
}

