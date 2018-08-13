// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

/*


Profiles

Profiles describe the network behaviour


Profiles are found in 3 different paths:
- /Me/Profiles/: Profiles used for this system
- /Data/Profiles/: Profiles supplied by Safing
- /Company/Profiles/: Profiles supplied by the company

When a program wants to use the network for the first time, Safing first searches for a Profile in the Company namespace, then in the Data namespace. If neither is found, it searches for a default profile in the same order.

Default profiles are profiles with a path ending with a "/". The default profile with the longest matching path is chosen.

*/
package process
