import { CommonModule } from "@angular/common";
import { NgModule } from "@angular/core";
import { SfngAccordionComponent } from "./accordion";
import { SfngAccordionGroupComponent } from "./accordion-group";

@NgModule({
  imports: [
    CommonModule,
  ],
  declarations: [
    SfngAccordionGroupComponent,
    SfngAccordionComponent,
  ],
  exports: [
    SfngAccordionGroupComponent,
    SfngAccordionComponent,
  ]
})
export class SfngAccordionModule { }
