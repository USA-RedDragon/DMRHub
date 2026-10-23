# i10-flaky (lane/i10-flaky): finished
- 33ba2708 hub: deliver nothing for a repeater once its subscriptions are stopped. TestStopAll was a REAL code bug: a subscription busy in its repeater lookup when Stop ran still delivered after Stop returned. Proven by holding 20 lookups in a gorm callback across Stop: 15/15 failed before ("repeater N received stream 90100 after Stop returned"). Commit 2f235be had hidden it (the packet it checks is refused by the routing queue after Stop).
- a358088f hub: subscribe a repeater before ActivateRepeater returns. Subscriptions were set up asynchronously, so bursts routed in that gap were lost; new test failed 3/3 before.
- 0054b53c hub: wait for the websocket listener to subscribe in its tests. TestListenForWebsocket was a test timing bug, not a stall: ListenForWebsocket subscribes after loading the user, test slept 100 ms; with a 300 ms lookup delay the old test failed 5/5. Voice forwarding path makes no DB call.
- After: 250/250 (GOMAXPROCS=1, -race, load ~60); hub package -race passes; hub lint 0 issues.
- Left: dependent packages, go build/vet ./..., whole-repo lint not rerun (disk full at the time).
- Open finding, not fixed: in the ctx.Done branch, subscribeRepeater and subscribeTG delete their map entry by key; after a ReloadRepeater that can remove the NEW subscription's entry and leak it.
