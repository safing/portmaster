import { PortalModule } from "@angular/cdk/portal";
import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { BrowserAnimationsModule } from "@angular/platform-browser/animations";
import { SfngTipUpModule } from "../tipup";
import { SfngTabComponent, SfngTabContentDirective, TabOutletComponent } from "./tab";
import { SfngTabGroupComponent } from "./tab-group";

@NgModule({
  imports: [
    CommonModule,
    PortalModule,
    SfngTipUpModule,
    BrowserAnimationsModule
  ],
  declarations: [
    SfngTabContentDirective,
    SfngTabComponent,
    SfngTabGroupComponent,
    TabOutletComponent,
  ],
  exports: [
    SfngTabContentDirective,
    SfngTabComponent,
    SfngTabGroupComponent
  ]
})
export class SfngTabModule { }
