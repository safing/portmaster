import { ChangeDetectionStrategy, Component } from "@angular/core";

@Component({
  selector: 'ext-header',
  templateUrl: './header.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrls: ['./header.component.scss']
})
export class ExtHeaderComponent { }
