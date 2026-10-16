# Tests de la bibliothèque

Les commandes se lancent depuis la racine du dépôt.

```powershell
mise run test
```

Cette commande installe les dépendances Go et le SDK JavaScript fixé dans `sdk/package-lock.json`, puis exécute les tests Go et le test OAuth avec le SDK. `mise run check` ajoute l’analyse Go, la compilation de la bibliothèque et la vérification de syntaxe JavaScript.

## Où se trouvent les tests ?

- Les fichiers `*_test.go` à la racine testent la bibliothèque, à côté de son code. Ils couvrent OAuth/OIDC, les identités privées et les règles, hooks, MFA et événements realtime de PocketBase.
- `tests/sdk/oauth.test.mjs` vérifie le parcours OAuth avec le SDK JavaScript officiel.
- `internal/mockshop` est le simulateur Shopify utilisé par les tests. Il expose localement les endpoints de découverte, OAuth, JWKS et GraphQL.

## Test OAuth avec le SDK officiel

`sdk/oauth.test.mjs` exécute les fonctions OAuth du [README de la bibliothèque](../README.md#exemple-frontend-minimal). Il vérifie les connexions silencieuse et interactive, le state, le stockage temporaire et le refus du rejeu, ainsi que la mise à jour de l’auth store, la persistance et le renouvellement de la session.

`sdk_integration_test.go` démarre des serveurs HTTP et des bases SQLite temporaires, puis lance le script Node en lui transmettant les paramètres par l’entrée standard. Ce script n’est pas une commande à lancer seul.

Les tests Go conservent la couverture des opérations CRUD et des règles de propriétaire avec les comptes Shopify et natifs. Le test JavaScript se concentre sur la compatibilité OAuth avec le SDK.

Le SDK et les échanges HTTP sont réels. La navigation et le stockage du navigateur sont simulés dans Node. Ces tests ne remplacent pas un essai dans un navigateur ou une connexion à la vraie boutique Shopify. Ils n’utilisent aucune donnée client ni configuration Shopify locale.

## Exécuter seulement les tests Go

```powershell
mise exec -- go test ./... -count=1
```

Sans `RUN_JS_SDK_TEST=1`, le test OAuth avec le SDK est ignoré. Node est nécessaire pour la suite complète ; il n’est pas nécessaire pour utiliser la bibliothèque Go.

La dépendance npm de `sdk/package.json` sert uniquement aux tests de cette bibliothèque.

La démo et ses tests sont dans le projet voisin `shopify-pocketbase-sso-demo`. Aucun fichier de ce projet voisin n’est nécessaire pour lancer les tests de la bibliothèque.
