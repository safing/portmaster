import { Pipe, PipeTransform } from '@angular/core';
import { AppProfile, FingerprintType, FingerpringOperation } from '@safing/portmaster-api';

/**
 * Returns a single-line tooltip describing the process matching rule of a profile.
 * Only shown when the profile has exactly one fingerprint of type Path or Cmdline.
 * Examples: "/usr/bin/app", "[Path:Regex] .*firefox.*", "[Command] /usr/bin/python3 script.py", "[Command:Regex] .*--profile.*"
 */
@Pipe({ name: 'appFingerprint' })
export class AppFingerprintPipe implements PipeTransform {
  transform(profile: AppProfile): string | null {
    if (profile?.Fingerprints?.length !== 1) return null;

    const { Type, Operation, Value } = profile.Fingerprints[0];
    const opSuffix = Operation === FingerpringOperation.Equal
      ? ''
      : `:${Operation.charAt(0).toUpperCase()}${Operation.slice(1)}`;

    if (Type === FingerprintType.Path) {
      return Operation === FingerpringOperation.Equal
        ? Value
        : `[路径:${Operation.charAt(0).toUpperCase()}${Operation.slice(1)}] ${Value}`;
    }

    if (Type === FingerprintType.Cmdline) {
      return `[命令${opSuffix}] ${Value}`;
    }

    return null;
  }
}
