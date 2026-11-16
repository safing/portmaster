import { CommonModule } from '@angular/common';
import { NgModule } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { I18nModule } from 'src/i18n';
import { SPNModule } from '../spn/spn.module';
import { XrayPageComponent } from './xray-page';

@NgModule({
  imports: [
    CommonModule,
    FormsModule,
    SPNModule,
    I18nModule,
  ],
  declarations: [
    XrayPageComponent,
  ],
  exports: [
    XrayPageComponent,
  ],
})
export class XrayModule { }
