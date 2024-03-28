import { ChangeDetectionStrategy, Component } from "@angular/core";
import { Step } from "@safing/ui";
import { of } from "rxjs";

@Component({
  templateUrl: './step-4-tipups.html',
  styleUrls: ['../step.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class Step4TipupsComponent implements Step {
  validChange = of(true)
}
