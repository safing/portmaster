package portscan

/*
* delay start by 1 Minutes (in order to let answer-packets from old sockets arrive (at reboot))
* if Portscan detected: secure mode; IP-Block
* Whitelist outgoing connections
* Whitelist DHCP, ICMP, IGM, NetBios, foreign destination IPs (including especially Broadcast&Multicast)
* Score >= 160: Portscan; set previous offender-flag which is persistent until 24 hours of inactivity
* ability to set ignore-flag (persistent until 24 hours of inactivity)
* previous offender is blocked on 1st probed closed port

flowchart:
----------

function inspect() {
	if can't get IP {
	return undecided;
	}

	if IP listed {
		call updateIPstate();
		update last seen;

		if IP ignored {
			return undecided;
		}
	}

	if no process attached
	&& inbound && tcp/udp
	&& going to own singlecast-address
	&& not NetBIOS over TCP/IP
	&& not DHCP {
		call handleMaliciousPacket();
	}

	return blocked if blocked, otherwise undecided;
}

function updateIPstate() {
  recalculate score;
  reset ignore-flag if expired;
  reset block-flag if expired and delete own threat;
  update lastUpdated;
  if nothing important in entry{
    delete entry;
  }
}

function handleMaliciousPacket() {
	set score depending on type of port;

	if IP not listed listed {
	  add to List;
	  return;
	}

	if probed port is not in th List of already ports by that IP {
	  add to List of Ports;
	  update score;
	  update wether IP is is blocked;

	  if blocked and no threat-warning {
	    create threat-warning;
	  }
	}
} */
