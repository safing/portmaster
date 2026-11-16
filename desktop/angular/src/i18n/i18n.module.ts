import { CommonModule } from '@angular/common';
import { NgModule } from '@angular/core';
import { TranslatePipe } from './translate.pipe';

@NgModule({
  imports: [CommonModule],
  declarations: [TranslatePipe],
  exports: [TranslatePipe],
})
export class I18nModule { }
