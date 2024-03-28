import { NgModule } from '@angular/core';
import { CountryFlagDirective } from './country-flag';

@NgModule({
  declarations: [
    CountryFlagDirective
  ],
  exports: [
    CountryFlagDirective,
  ]
})
export class CountryFlagModule { }
