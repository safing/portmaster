import { NgModule } from "@angular/core";
import { CountIndicatorComponent } from "./count-indicator";
import { PrettyCountPipe } from "./count.pipe";

@NgModule({
  declarations: [
    CountIndicatorComponent,
    PrettyCountPipe,
  ],
  exports: [
    CountIndicatorComponent,
    PrettyCountPipe,
  ]
})
export class CountIndicatorModule { }
