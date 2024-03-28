import { A11yModule } from '@angular/cdk/a11y';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { OverlayModule } from '@angular/cdk/overlay';
import { CommonModule } from '@angular/common';
import { NgModule } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterModule } from '@angular/router';
import { FontAwesomeModule } from '@fortawesome/angular-fontawesome';
import { SfngToggleSwitchModule, SfngTooltipModule, TabModule } from '@safing/ui';
import { SfngAppIconModule } from 'src/app/shared/app-icon';
import { CountIndicatorModule } from 'src/app/shared/count-indicator';
import { CountryFlagModule } from 'src/app/shared/country-flag';
import { ExpertiseModule } from 'src/app/shared/expertise/expertise.module';
import { SfngFocusModule } from 'src/app/shared/focus';
import { SfngMenuModule } from 'src/app/shared/menu';
import { CommonPipesModule } from 'src/app/shared/pipes';
import { SpnPageComponent } from './';
import { CountryDetailsComponent } from './country-details';
import { CountryOverlayComponent } from './country-overlay';
import { SpnMapLegendComponent } from './map-legend';
import { MapRendererComponent } from './map-renderer';
import { SpnNodeIconComponent } from './node-icon';
import { PinDetailsComponent } from './pin-details';
import { SpnPinListComponent } from './pin-list/pin-list';
import { PinOverlayComponent } from './pin-overlay';
import { SpnPinRouteComponent } from './pin-route';
import { SPNFeatureCarouselComponent } from './spn-feature-carousel';

@NgModule({
  imports: [
    CommonModule,
    FormsModule,
    CountryFlagModule,
    SfngTooltipModule,
    SfngMenuModule,
    SfngFocusModule,
    SfngAppIconModule,
    SfngToggleSwitchModule,
    TabModule,
    A11yModule,
    ExpertiseModule,
    OverlayModule,
    CountIndicatorModule,
    FontAwesomeModule,
    CommonPipesModule,
    DragDropModule,
    RouterModule,
  ],
  declarations: [
    MapRendererComponent,
    PinOverlayComponent,
    CountryOverlayComponent,
    CountryDetailsComponent,
    SpnNodeIconComponent,
    SpnMapLegendComponent,
    PinDetailsComponent,
    SpnPinRouteComponent,
    SPNFeatureCarouselComponent,
    SpnPageComponent,
    SpnPinListComponent,
  ],
  exports: [
    SpnPageComponent,
    SpnPinRouteComponent,
    SpnNodeIconComponent,
    MapRendererComponent,
  ]
})
export class SPNModule { }
