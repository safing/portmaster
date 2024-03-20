import { CdkScrollableModule } from "@angular/cdk/scrolling";
import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { FormsModule, ReactiveFormsModule } from "@angular/forms";
import { SfngDropDownModule } from "../dropdown";
import { SfngTooltipModule } from "../tooltip";
import { SfngSelectItemComponent, SfngSelectValueDirective } from "./item";
import { SfngSelectComponent, SfngSelectRenderedItemDirective } from "./select";

@NgModule({
  imports: [
    CommonModule,
    FormsModule,
    ReactiveFormsModule,
    SfngDropDownModule,
    SfngTooltipModule,
    CdkScrollableModule
  ],
  declarations: [
    SfngSelectComponent,
    SfngSelectValueDirective,
    SfngSelectItemComponent,
    SfngSelectRenderedItemDirective
  ],
  exports: [
    SfngSelectComponent,
    SfngSelectValueDirective,
    SfngSelectItemComponent,
  ]
})
export class SfngSelectModule { }
