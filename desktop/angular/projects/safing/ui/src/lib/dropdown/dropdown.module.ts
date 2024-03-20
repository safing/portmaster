import { OverlayModule } from "@angular/cdk/overlay";
import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { SfngDropdownComponent } from "./dropdown";

@NgModule({
  imports: [
    CommonModule,
    OverlayModule,
  ],
  declarations: [
    SfngDropdownComponent,
  ],
  exports: [
    SfngDropdownComponent,
  ]
})
export class SfngDropDownModule { }
