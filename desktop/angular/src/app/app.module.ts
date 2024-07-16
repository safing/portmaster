import { DragDropModule } from '@angular/cdk/drag-drop';
import { OverlayModule } from '@angular/cdk/overlay';
import { PortalModule } from '@angular/cdk/portal';
import { ScrollingModule } from '@angular/cdk/scrolling';
import { CdkTableModule } from '@angular/cdk/table';
import { CommonModule, registerLocaleData } from '@angular/common';
import { HttpClientModule } from '@angular/common/http';

import { APP_INITIALIZER, LOCALE_ID, NgModule } from '@angular/core';
import { FormsModule, ReactiveFormsModule } from '@angular/forms';
import { BrowserModule } from '@angular/platform-browser';
import { BrowserAnimationsModule } from '@angular/platform-browser/animations';
import { FaIconLibrary, FontAwesomeModule } from '@fortawesome/angular-fontawesome';
import { faGithub } from '@fortawesome/free-brands-svg-icons';
import { far } from '@fortawesome/free-regular-svg-icons';
import { fas } from '@fortawesome/free-solid-svg-icons';
import { ConfigService, PortmasterAPIModule, StringSetting, getActualValue } from '@safing/portmaster-api';
import { OverlayStepperModule, SfngAccordionModule, SfngDialogModule, SfngDropDownModule, SfngPaginationModule, SfngSelectModule, SfngTipUpModule, SfngToggleSwitchModule, SfngTooltipModule, TabModule, UiModule } from '@safing/ui';
import MyYamlFile from 'js-yaml-loader!../i18n/helptexts.yaml';
import * as i18n from 'ng-zorro-antd/i18n';
import { MarkdownModule } from 'ngx-markdown';
import { firstValueFrom } from 'rxjs';
import { environment } from 'src/environments/environment';
import { AppRoutingModule } from './app-routing.module';
import { AppComponent } from './app.component';
import { IntroModule } from './intro';
import { NavigationComponent } from './layout/navigation/navigation';
import { SideDashComponent } from './layout/side-dash/side-dash';
import { AppOverviewComponent, AppViewComponent, QuickSettingInternetButtonComponent } from './pages/app-view';
import { QsHistoryComponent } from './pages/app-view/qs-history/qs-history.component';
import { QuickSettingSelectExitButtonComponent } from './pages/app-view/qs-select-exit/qs-select-exit';
import { QuickSettingUseSPNButtonComponent } from './pages/app-view/qs-use-spn/qs-use-spn';
import { DashboardPageComponent } from './pages/dashboard/dashboard.component';
import { FeatureCardComponent } from './pages/dashboard/feature-card/feature-card.component';
import { MonitorPageComponent } from './pages/monitor';
import { SettingsComponent } from './pages/settings/settings';
import { SPNModule } from './pages/spn/spn.module';
import { SupportPageComponent } from './pages/support';
import { SupportFormComponent } from './pages/support/form';
import { NotificationsService } from './services';
import { ActionIndicatorModule } from './shared/action-indicator';
import { SfngAppIconModule } from './shared/app-icon';
import { ConfigModule } from './shared/config';
import { CountIndicatorModule } from './shared/count-indicator';
import { CountryFlagModule } from './shared/country-flag';
import { EditProfileDialog } from './shared/edit-profile-dialog';
import { ExitScreenComponent } from './shared/exit-screen/exit-screen';
import { ExpertiseModule } from './shared/expertise/expertise.module';
import { ExternalLinkDirective } from './shared/external-link.directive';
import { FeatureScoutComponent } from './shared/feature-scout';
import { SfngFocusModule } from './shared/focus';
import { FuzzySearchPipe } from './shared/fuzzySearch';
import { LoadingComponent } from './shared/loading';
import { SfngMenuModule } from './shared/menu';
import { SfngMultiSwitchModule } from './shared/multi-switch';
import { NetqueryModule } from './shared/netquery';
import { NetworkScoutComponent } from './shared/network-scout';
import { NotificationListComponent } from './shared/notification-list/notification-list.component';
import { NotificationComponent } from './shared/notification/notification';
import { CommonPipesModule } from './shared/pipes';
import { ProcessDetailsDialogComponent } from './shared/process-details-dialog';
import { PromptListComponent } from './shared/prompt-list/prompt-list.component';
import { SecurityLockComponent } from './shared/security-lock';
import { SPNAccountDetailsComponent } from './shared/spn-account-details';
import { SPNLoginComponent } from './shared/spn-login';
import { SPNStatusComponent } from './shared/spn-status';
import { PlaceholderComponent } from './shared/text-placeholder';
import { DashboardWidgetComponent } from './pages/dashboard/dashboard-widget/dashboard-widget.component';
import { MergeProfileDialogComponent } from './pages/app-view/merge-profile-dialog/merge-profile-dialog.component';
import { AppInsightsComponent } from './pages/app-view/app-insights/app-insights.component';
import { INTEGRATION_SERVICE, integrationServiceFactory } from './integration';
import { SupportProgressDialogComponent } from './pages/support/progress-dialog';

function loadAndSetLocaleInitializer(configService: ConfigService) {
  return async function () {
    let angularLocaleID = 'en-GB';
    let nzLocaleID: string = 'en_GB';

    try {
      const setting = await firstValueFrom(configService.get("core/locale"))

      const currentValue = getActualValue(setting as StringSetting);
      switch (currentValue) {
        case 'en-US':
          angularLocaleID = 'en-US'
          nzLocaleID = 'en_US'
          break;
        case 'en-GB':
          angularLocaleID = 'en-GB'
          nzLocaleID = 'en_GB'
          break;

        default:
          console.error(`Unsupported locale value: ${currentValue}, defaulting to en-GB`)
      }
    } catch (err) {
      console.error(`failed to get locale setting, using default en-GB:`, err)
    }

    try {
      // Get name of module.
      let localeModuleID = angularLocaleID;
      if (localeModuleID == "en-US") {
        localeModuleID = "en";
      }

      /* webpackInclude: /(en|en-GB)\.mjs$/ */
      /* webpackChunkName: "./l10n-base/[request]"*/
      await import(`../../node_modules/@angular/common/locales/${localeModuleID}.mjs`)
        .then(locale => {
          registerLocaleData(locale.default)

          localeConfig.localeId = angularLocaleID;
          localeConfig.nzLocale = (i18n as any)[nzLocaleID];
        })
    } catch (err) {
      console.error(`failed to load locale module for ${angularLocaleID}:`, err)
    }
  }
}

const localeConfig = {
  nzLocale: i18n.en_GB,
  localeId: 'en-GB'
}

@NgModule({
  declarations: [
    AppComponent,
    NotificationComponent,
    SettingsComponent,
    MonitorPageComponent,
    SideDashComponent,
    NavigationComponent,
    NotificationListComponent,
    PromptListComponent,
    FuzzySearchPipe,
    AppViewComponent,
    QuickSettingInternetButtonComponent,
    QuickSettingUseSPNButtonComponent,
    QuickSettingSelectExitButtonComponent,
    AppOverviewComponent,
    PlaceholderComponent,
    LoadingComponent,
    ExternalLinkDirective,
    ExitScreenComponent,
    SupportPageComponent,
    SupportFormComponent,
    SecurityLockComponent,
    SPNStatusComponent,
    FeatureScoutComponent,
    SPNLoginComponent,
    SPNAccountDetailsComponent,
    NetworkScoutComponent,
    EditProfileDialog,
    ProcessDetailsDialogComponent,
    QsHistoryComponent,
    DashboardPageComponent,
    DashboardWidgetComponent,
    FeatureCardComponent,
    MergeProfileDialogComponent,
    AppInsightsComponent,
    SupportProgressDialogComponent
  ],
  imports: [
    BrowserModule,
    CommonModule,
    BrowserAnimationsModule,
    FormsModule,
    ReactiveFormsModule,
    AppRoutingModule,
    FontAwesomeModule,
    OverlayModule,
    PortalModule,
    CdkTableModule,
    DragDropModule,
    HttpClientModule,
    MarkdownModule.forRoot(),
    ScrollingModule,
    SfngAccordionModule,
    TabModule,
    SfngTipUpModule.forRoot(MyYamlFile, NotificationsService),
    SfngTooltipModule,
    ActionIndicatorModule,
    SfngDialogModule,
    OverlayStepperModule,
    IntroModule,
    SfngDropDownModule,
    SfngSelectModule,
    SfngMultiSwitchModule,
    SfngMenuModule,
    SfngFocusModule,
    SfngToggleSwitchModule,
    SfngPaginationModule,
    SfngAppIconModule,
    ExpertiseModule,
    ConfigModule,
    CountryFlagModule,
    CountIndicatorModule,
    NetqueryModule,
    CommonPipesModule,
    UiModule,
    SPNModule,
    PortmasterAPIModule.forRoot({
      httpAPI: environment.httpAPI,
      websocketAPI: environment.portAPI,
    }),
  ],
  bootstrap: [AppComponent],
  providers: [
    {
      provide: APP_INITIALIZER, useFactory: loadAndSetLocaleInitializer, deps: [ConfigService], multi: true
    },
    {
      provide: i18n.NZ_I18N, useFactory: () => {
        console.log("nz-locale is set to", localeConfig.nzLocale)
        return localeConfig.nzLocale
      }
    },
    {
      provide: LOCALE_ID, useFactory: () => {
        console.log("locale-id is set to", localeConfig.localeId)
        return localeConfig.localeId
      }
    },
    {
      provide: INTEGRATION_SERVICE,
      useFactory: integrationServiceFactory
    }
  ]
})
export class AppModule {
  constructor(library: FaIconLibrary) {
    library.addIconPacks(fas, far);
    library.addIcons(faGithub)
  }
}

