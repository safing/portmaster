import { OverlayModule } from "@angular/cdk/overlay";
import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { SfngDropDownModule } from "@safing/ui";
import { MenuComponent, MenuGroupComponent, MenuItemComponent, MenuTriggerComponent } from "./menu";

@NgModule({
  imports: [
    SfngDropDownModule,
    CommonModule,
    OverlayModule,
  ],
  declarations: [
    MenuComponent,
    MenuGroupComponent,
    MenuTriggerComponent,
    MenuItemComponent,
  ],
  exports: [
    MenuComponent,
    MenuGroupComponent,
    MenuTriggerComponent,
    MenuItemComponent,
  ],
})
export class SfngMenuModule { }
