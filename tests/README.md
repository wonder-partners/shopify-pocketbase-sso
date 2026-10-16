# Tests

Les commandes se lancent depuis la racine du dépôt.

```powershell
mise run test
```

Cette commande installe les dépendances Go et le SDK JavaScript fixé dans `sdk/package-lock.json`, puis exécute tous les tests Go avec les trois tests SDK activés. `mise run check` ajoute l’analyse Go, la compilation de la démo, la syntaxe JavaScript et le contrôle du diff.

## Où se trouvent les tests ?

- Les fichiers `*_test.go` à la racine testent la bibliothèque, à côté de son code. Ils couvrent OAuth/OIDC, les identités privées et les règles, hooks, MFA et événements realtime de PocketBase.
- `examples/demo/main_test.go` vérifie les pages et leurs en-têtes, leur parité avec le README et leur utilisation du SDK.
- `examples/demo/migrations/*_test.go` vérifie les migrations du prototype, conservées avec la démo.
- `tests/sdk/*.test.mjs` vérifie l’utilisation du backend et de la démo par le SDK JavaScript officiel.
- `internal/mockshop` est le simulateur Shopify utilisé par les tests. Il expose localement les endpoints de découverte, OAuth, JWKS et GraphQL.

## Les trois tests SDK

- `sdk/sdk.test.mjs` vérifie l’auth store, le renouvellement des tokens, les opérations CRUD et les règles de propriétaire avec des comptes Shopify et des comptes natifs.
- `sdk/oauth.test.mjs` exécute les fonctions OAuth du [README de la bibliothèque](../README.md#exemple-frontend-minimal). Il vérifie les connexions silencieuse et interactive, le state, le stockage temporaire et le refus du rejeu.
- `sdk/demo.test.mjs` exécute le JavaScript de `examples/demo/pb_public` et vérifie les pages de connexion et de callback, la persistance de session, la déconnexion et l’affichage des erreurs.

Les tests Go démarrent des serveurs HTTP et des bases SQLite temporaires, puis lancent les scripts Node en leur transmettant les paramètres par l’entrée standard. `sdk_integration_test.go` lance les deux premiers ; `examples/demo/main_test.go` lance le troisième. Ces scripts ne sont pas des commandes à lancer seuls.

Le SDK et les échanges HTTP sont réels. Le DOM, la navigation et le stockage du navigateur sont simulés dans Node. Ces tests ne remplacent pas un essai dans un navigateur ou une connexion à la vraie boutique Shopify. Ils n’utilisent aucune donnée client ni configuration Shopify locale.

## Exécuter seulement les tests Go

```powershell
mise exec -- go test ./... -count=1
```

Sans `RUN_JS_SDK_TEST=1`, les trois tests SDK sont ignorés. Node est nécessaire pour la suite complète ; il n’est pas nécessaire pour utiliser la bibliothèque Go.

La dépendance npm de `sdk/package.json` sert uniquement aux tests. Sa version est également vérifiée contre celle importée par les pages de la démo.

Les comptes rendus des validations précédentes sont conservés dans [docs/history](../docs/history/README.md).
