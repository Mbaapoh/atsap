# Tasks — outbound destination classes

## 1. Domain and storage
- [x] 1.1 Add `OutboundClass` with ranking, default and dialled-form classification
- [x] 1.2 Add migration 0019 and persist the class on insert/update/read/list
- [x] 1.3 Extend extension commands, views and the create/update use cases

## 2. Enforcement
- [x] 2.1 Refuse a trunk destination outside the caller's class in `PlaceCall`, emergency exempt
- [x] 2.2 Map the refusal to a fixed permission-denied API error

## 3. Wire and console
- [x] 3.1 Extend the proto and regenerate Go, TypeScript and OpenAPI
- [x] 3.2 Show the class and offer the choice when adding/editing an extension

## 4. Proof
- [x] 4.1 Domain, RPC and integration tests for default, refusal, widening and emergency
- [x] 4.2 Console tests; full integration, full e2e, console-check, docs and SCA
