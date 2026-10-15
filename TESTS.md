# Validation après retrait des couches de démonstration

Environnement : Windows amd64, Go 1.27.2, PocketBase 0.40.5, SDK JavaScript officiel 0.28.1.

```powershell
npm ci --prefix integration --ignore-scripts --no-audit --no-fund
$env:RUN_JS_SDK_TEST = '1'
go test ./... -count=1 -cover
go vet ./...
go build ./...
node --check integration/oauth.mjs
node --check integration/sdk.mjs
```

Résultat du 15 octobre 2026 : succès des 47 groupes de tests du cœur, couverture de 86,3 %. Les cinq groupes des migrations historiques passent aussi, avec une couverture de 81,0 %. `go vet`, la compilation du backend et les vérifications de syntaxe JavaScript passent.

Les tests utilisent une instance HTTP PocketBase et une base SQLite temporaire par scénario. Le fournisseur Shopify simulé vérifie les codes à usage unique, PKCE et l'authentification éventuelle du client confidential. Il n'est ni importé par le code d'authentification, ni lancé comme boutique de démonstration.

## Contrôles actuels

- Parcours JSON `start` puis `complete`, suivi du véritable endpoint OAuth natif.
- Session Shopify existante, visiteur anonyme avec `prompt=none`, puis parcours interactif sans HTML fourni par le service.
- Contrôles d'origine avant la création ou consommation du parcours ; state, cookie du navigateur, expiration et rejeu.
- Signature, algorithme, audience, émetteur, nonce, expiration et `nbf` ; sujets numériques conservés sans arrondi.
- Aucun cookie de session ni token dans la réponse de préparation. Seul le cookie temporaire de parcours est créé puis expiré.
- Anciennes pages, scripts, `/studio/session` et `/studio/logout` absents du routeur.
- Un ancien cookie contenant un vrai jeton PocketBase n'authentifie pas les API ; l'en-tête natif reste nécessaire.
- Hooks OAuth, création, auth et enrichissement ; refus avant ou après `e.Next()` sans fuite de JWT.
- Règles natives, relations de propriétaire, MFA, renouvellement et filtrage realtime, comparés aux comptes avec mot de passe.
- Comptes et liaisons OAuth existants conservés ; aucun rapprochement automatique par e-mail ou en-tête Authorization.
- Schéma et règles de `users` laissés au projet hôte, y compris les champs portant d'anciens noms du prototype.
- Collection `_shopifyIdentities` système, API verrouillée, identifiants bruts non exposés, suppression par cascade depuis `users`.

## SDK officiel sans pont de session

`integration/oauth.mjs` exécute `authWithOAuth2Code`, vérifie `authStore.isValid`, le compte et la notification `onChange`, puis crée une nouvelle instance du SDK. Cette instance retrouve l'état dans le `LocalAuthStore` par défaut, effectue un `authRefresh` natif et lit son propre profil. Enfin, `authStore.clear()` rend une nouvelle instance non authentifiée.

Le test fournit à Node une simulation du contrat Web Storage de navigateur ; il n'utilise pas de custom auth store. Il ne remplace pas un test dans un navigateur réel. Les requêtes sont surveillées pour refuser toute route autre que les API natives de collections et tout cookie de session ajouté. Aucune requête à `/studio/session` n'est possible dans ce test.

`integration/sdk.mjs` vérifie aussi les règles CRUD et le renouvellement avec un compte Shopify et un compte natif. Ces deux tests sont ignorés sans `RUN_JS_SDK_TEST=1`.

## Historique et données existantes

Les migrations historiques restent testées séparément, sans être importées par `main.go` ou par le cœur d'authentification. Le nettoyage du code ne supprime ni la base `pb_data`, ni les comptes, ni les sauvegardes, ni l'app Shopify existante. L'ancienne interface de démonstration et sa gateway ne font plus partie du projet exécutable.

Les validations antérieures contre la vraie boutique sont conservées dans [docs/prototype-tests.md](docs/prototype-tests.md). Le nouveau contrat JSON n'a pas encore été testé contre Shopify depuis un frontend hôte, puisque cette interface ne fait plus partie du projet. Les tests automatisés valident le contrat local et le SDK, pas les serveurs Shopify.

Le backend nettoyé a été démarré en local sur `127.0.0.1:8090`. Les contrôles HTTP confirment la santé de l'API, les méthodes d'auth natives et le démarrage JSON avec `prompt=none`, PKCE S256, le callback enregistré et uniquement un cookie temporaire. Les anciennes routes retournent HTTP 404 ; une origine étrangère est refusée avec HTTP 403.

Les processus de l'ancienne interface, de sa gateway et de son tunnel public ont été arrêtés. La sauvegarde SQLite `pb_backups/before-demo-stop-20261015.db` précède cet arrêt. Une comparaison après redémarrage du backend confirme que `users`, `_shopifyIdentities` et `_externalAuths` sont inchangés. Le contrôle SQLite `quick_check` retourne `ok`. L'app Shopify et ses fichiers locaux n'ont pas été modifiés.
