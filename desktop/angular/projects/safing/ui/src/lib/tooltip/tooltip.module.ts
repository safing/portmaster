import { OverlayModule } from "@angular/cdk/overlay";
import { PortalModule } from "@angular/cdk/portal";
import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { SfngTooltipDirective } from "./tooltip";
import { SfngTooltipComponent } from "./tooltip-component";

@NgModule({
  imports: [
    PortalModule,
    OverlayModule,
    CommonModule,
  ],
  declarations: [
    SfngTooltipDirective,
    SfngTooltipComponent
  ],
  exports: [
    SfngTooltipDirective
  ]
})
export class SfngTooltipModule { }

