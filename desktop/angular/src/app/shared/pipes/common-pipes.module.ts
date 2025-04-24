import { NgModule } from "@angular/core";
import { BytesPipe } from "./bytes.pipe";
import { TimeAgoPipe } from "./time-ago.pipe";
import { ToAppProfilePipe } from "./to-profile.pipe";
import { DurationPipe } from "./duration.pipe";
import { RoundPipe } from "./round.pipe";
import { ToSecondsPipe } from "./to-seconds.pipe";

@NgModule({
  declarations: [
    TimeAgoPipe,
    BytesPipe,
    ToAppProfilePipe,
    DurationPipe,
    RoundPipe,
    ToSecondsPipe
  ],
  exports: [
    TimeAgoPipe,
    BytesPipe,
    ToAppProfilePipe,
    DurationPipe,
    RoundPipe,
    ToSecondsPipe
  ]
})
export class CommonPipesModule { }
