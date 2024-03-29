import { DragDropModule } from '@angular/cdk/drag-drop';
import { CommonModule } from '@angular/common';
import { NgModule } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterModule } from '@angular/router';
import { FontAwesomeModule } from '@fortawesome/angular-fontawesome';
import {
  SfngSelectModule,
  SfngTipUpModule,
  SfngToggleSwitchModule,
  SfngTooltipModule,
} from '@safing/ui';
import { MarkdownModule } from 'ngx-markdown';
import { ExpertiseModule } from '../expertise/expertise.module';
import { SfngFocusModule } from '../focus';
import { SfngMenuModule } from '../menu';
import { SfngMultiSwitchModule } from '../multi-switch';
import { BasicSettingComponent } from './basic-setting/basic-setting';
import { ConfigSettingsViewComponent } from './config-settings';
import { FilterListComponent } from './filter-lists';
import { GenericSettingComponent } from './generic-setting';
import {
  OrderedListComponent,
  OrderedListItemComponent,
} from './ordererd-list';
import { RuleListItemComponent } from './rule-list/list-item';
import { RuleListComponent } from './rule-list/rule-list';
import { SafePipe } from './safe.pipe';
import { ExportDialogComponent } from './export-dialog/export-dialog.component';
import { ImportDialogComponent } from './import-dialog/import-dialog.component';
import { SfngAppIconModule } from '../app-icon';

@NgModule({
  imports: [
    CommonModule,
    FormsModule,
    DragDropModule,
    SfngTooltipModule,
    SfngSelectModule,
    SfngMultiSwitchModule,
    SfngFocusModule,
    SfngMenuModule,
    SfngTipUpModule,
    FontAwesomeModule,
    MarkdownModule,
    RouterModule,
    ExpertiseModule,
    SfngToggleSwitchModule,
    MarkdownModule,
    SfngAppIconModule
  ],
  declarations: [
    BasicSettingComponent,
    FilterListComponent,
    OrderedListComponent,
    OrderedListItemComponent,
    RuleListComponent,
    RuleListItemComponent,
    ConfigSettingsViewComponent,
    GenericSettingComponent,
    SafePipe,
    ExportDialogComponent,
    ImportDialogComponent,
  ],
  exports: [
    BasicSettingComponent,
    FilterListComponent,
    OrderedListComponent,
    OrderedListItemComponent,
    RuleListComponent,
    RuleListItemComponent,
    ConfigSettingsViewComponent,
    GenericSettingComponent,
    SafePipe,
  ],
})
export class ConfigModule { }
