import { A11yModule } from "@angular/cdk/a11y";
import { OverlayModule } from "@angular/cdk/overlay";
import { CommonModule } from "@angular/common";
import { inject, NgModule } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { FontAwesomeModule } from "@fortawesome/angular-fontawesome";
import { SfngAccordionModule, SfngDropDownModule, SfngPaginationModule, SfngSelectModule, SfngTipUpModule, SfngToggleSwitchModule, SfngTooltipModule } from "@safing/ui";
import { NzDatePickerModule } from 'ng-zorro-antd/date-picker';
import { SfngAppIconModule } from "../app-icon";
import { CountIndicatorModule } from "../count-indicator";
import { CountryFlagModule } from "../country-flag";
import { ExpertiseModule } from "../expertise/expertise.module";
import { SfngFocusModule } from "../focus";
import { SfngMenuModule } from "../menu";
import { CommonPipesModule } from "../pipes";
import { SPNModule } from './../../pages/spn/spn.module';
import { SfngNetqueryAddToFilterDirective } from "./add-to-filter";
import { CombinedMenuPipe } from "./combined-menu.pipe";
import { SfngNetqueryConnectionDetailsComponent } from "./connection-details";
import { SfngNetqueryConnectionRowComponent } from "./connection-row";
import { SfngNetqueryLineChartComponent } from "./line-chart/line-chart";
import { SfngNetqueryViewer } from "./netquery.component";
import { CanShowConnection, CanUseRulesPipe, ConnectionLocationPipe, CountryNamePipe, CountryNameService, IsBlockedConnectionPipe } from "./pipes";
import { SfngNetqueryScopeLabelComponent } from "./scope-label";
import { SfngNetquerySearchOverlayComponent } from "./search-overlay";
import { SfngNetquerySearchbarComponent, SfngNetquerySuggestionDirective } from "./searchbar";
import { SfngNetqueryTagbarComponent } from "./tag-bar";
import { CircularBarChartComponent } from './circular-bar-chart/circular-bar-chart.component';

@NgModule({
  imports: [
    CommonModule,
    FormsModule,
    CountryFlagModule,
    SfngDropDownModule,
    SfngSelectModule,
    SfngTooltipModule,
    SfngAccordionModule,
    SfngMenuModule,
    SfngPaginationModule,
    SfngFocusModule,
    SfngAppIconModule,
    SfngTipUpModule,
    SfngToggleSwitchModule,
    A11yModule,
    ExpertiseModule,
    OverlayModule,
    CountIndicatorModule,
    FontAwesomeModule,
    CommonPipesModule,
    SPNModule,
    NzDatePickerModule,
  ],
  exports: [
    SfngNetqueryViewer,
    SfngNetqueryLineChartComponent,
    SfngNetquerySearchOverlayComponent,
    SfngNetqueryScopeLabelComponent,
    CircularBarChartComponent,
  ],
  declarations: [
    SfngNetqueryViewer,
    SfngNetqueryConnectionRowComponent,
    SfngNetqueryLineChartComponent,
    SfngNetqueryTagbarComponent,
    SfngNetquerySearchbarComponent,
    SfngNetquerySearchOverlayComponent,
    SfngNetquerySuggestionDirective,
    SfngNetqueryScopeLabelComponent,
    SfngNetqueryConnectionDetailsComponent,
    SfngNetqueryAddToFilterDirective,
    ConnectionLocationPipe,
    IsBlockedConnectionPipe,
    CanUseRulesPipe,
    CanShowConnection,
    CombinedMenuPipe,
    CircularBarChartComponent,
    CountryNamePipe,
  ],
  providers: [
    CountryNameService
  ]
})
export class NetqueryModule {
  private _unusedBootstrap = [
    inject(CountryNameService), // make sure country names are loaded on bootstrap
  ]
}
