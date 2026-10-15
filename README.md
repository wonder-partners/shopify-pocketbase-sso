# Authentification Shopify dans PocketBase

Le projet adapte les nouveaux comptes clients Shopify au fournisseur OAuth2 natif de PocketBase. La connexion se termine par `authWithOAuth2Code('shopify', ...)` du SDK officiel, qui reçoit un jeton PocketBase et alimente son auth store.

Le code est en cours de nettoyage. Le module Go `studio-sso` et le package `internal/studio` restent à leur place : aucun packaging, dépôt distant ou publication n'est préparé à ce stade.

## Responsabilités

- Shopify authentifie le client et fournit son identifiant et son e-mail.
- PocketBase crée ou retrouve le compte `users`, conserve la liaison OAuth dans `_externalAuths` et applique ses règles, hooks, MFA et enrichissements natifs.
- L'adaptation Go sécurise le parcours Shopify et conserve les identifiants bruts dans `_shopifyIdentities`.
- Le frontend hôte possède sa page de callback, sa navigation, ses messages et sa déconnexion locale. Le SDK gère `pb.authStore` et son stockage.

Il n'y a plus de page HTML du studio, de SDK embarqué, de cookie de session personnalisé, de `/studio/session` ni de `/studio/logout`. Le cookie temporaire `shopify_oauth`, préfixé `__Host-` en HTTPS, sert uniquement à lier le parcours OAuth au navigateur. Sa durée est de cinq minutes ; il ne contient aucun jeton d'authentification.

La mémoire tampon autour de la réponse OAuth est conservée comme protection : si un hook refuse la connexion après `e.Next()`, aucun JWT précédemment écrit ne doit parvenir au client. Elle ne fabrique aucun jeton, n'ajoute aucun cookie de session et conserve les réponses natives, notamment les challenges MFA.

## Configuration et hôte Go

`main.go` reste un hôte PocketBase minimal, sans interface de démonstration. Il initialise `studio.New` dans `OnServe`, puis appelle `Register` sur le routeur. La configuration active est :

| Paramètre Go | Variable du programme principal | Rôle |
| --- | --- | --- |
| `ShopURL` | `SHOPIFY_SHOP_URL` | Origine HTTPS de la boutique de confiance |
| `RedirectURL` | `SHOPIFY_REDIRECT_URL` | URL complète de la page de callback du frontend, enregistrée dans Shopify |
| `ClientID` | `SHOPIFY_CUSTOMER_CLIENT_ID` | Identifiant du client Customer Account API |
| `ClientSecret` | `SHOPIFY_CUSTOMER_CLIENT_SECRET` | Vide pour notre client public PKCE ; optionnel pour un client confidential |
| `AllowLoopbackHTTP` | `SSO_ALLOW_LOOPBACK_HTTP=1` | Exception de développement strictement limitée au loopback |

`STUDIO_URL` n'est plus lu. L'origine autorisée est déduite de `RedirectURL`. Cette version exige un frontend et une API sur la même origine. Il ne suffit pas de changer cette URL pour prendre en charge un frontend sur un domaine tiers.

La collection d'auth est fixée à `users`. Elle doit déjà exister et être de type auth. L'intégration ne crée pas cette collection, n'y ajoute pas de champ et ne choisit pas ses règles d'accès. Elle active OAuth2 et ajoute ou met à jour uniquement le fournisseur `shopify`, sans modifier les autres fournisseurs ni les politiques mot de passe, OTP, MFA ou durée des jetons.

La collection système `_shopifyIdentities` contient `user`, `issuer`, `subject` et `customerId`. Ses cinq règles API sont verrouillées. Les enregistrements restent accessibles au code serveur et aux superusers, pas aux utilisateurs par REST, expansion ou realtime. Les prénoms et noms ne sont ni demandés ni synchronisés.

Les anciens nettoyages de schéma du prototype ne sont plus dans le cœur d'authentification. Les migrations historiques du dossier `migrations` sont conservées mais ne sont plus importées par `main.go` et ne s'exécutent pas à l'installation de l'adaptation. La base de test existante a déjà été migrée ; ne pas réutiliser ces migrations de nettoyage sur un autre projet.

## Contrat HTTP du frontend

Deux endpoints JSON préparent la connexion. Ils ne constituent pas une API de session parallèle. Tous deux exigent un en-tête `Origin` correspondant à l'origine configurée et utilisent le cookie temporaire du même navigateur.

### 1. Démarrer le parcours

`POST /api/shopify/auth/start`, avec un corps JSON :

```json
{"silent": true}
```

La réponse est `{"url":"https://..."}`. Le frontend effectue lui-même une navigation de premier niveau vers cette URL. `silent: true` demande `prompt=none` à Shopify ; `false` permet une connexion interactive. Le serveur génère et conserve state, nonce et verifier PKCE.

Exemple avec une instance existante du SDK :

```javascript
const { url } = await pb.send('/api/shopify/auth/start', {
  method: 'POST',
  body: { silent: true },
});
window.location.assign(url);
```

### 2. Vérifier le retour Shopify

Shopify revient sur `RedirectURL`, une page fournie par le frontend hôte. Cette page doit extraire les paramètres puis les effacer de la barre d'adresse avant de charger d'autres ressources. Elle doit aussi empêcher la mise en cache et les fuites de référent de cette URL sensible.

Le frontend transmet `state`, `code` et, en cas d'échec Shopify, `error` à `POST /api/shopify/auth/complete`. Le serveur vérifie le navigateur, le state et son expiration, puis prépare le code à usage unique. Il renvoie les arguments nécessaires au SDK, jamais un jeton PocketBase.

En cas d'erreur OAuth attendue, la réponse JSON contient par exemple `{"error":"login_required"}`. L'application choisit alors d'afficher son lien de connexion ou de relancer un parcours interactif. L'adaptation ne choisit aucune page à sa place.

### 3. Authentifier avec le SDK officiel

Exemple de fonction appelée depuis la page de callback du frontend :

```javascript
async function finishShopifyLogin(pb) {
  const query = new URLSearchParams(window.location.search);
  history.replaceState(null, '', window.location.pathname);
  const completion = await pb.send('/api/shopify/auth/complete', {
    method: 'POST',
    body: {
      state: query.get('state') ?? '',
      code: query.get('code') ?? '',
      error: query.get('error') ?? '',
    },
  });
  if (completion.error) return completion;

  pb.authStore.clear();
  return pb.collection('users').authWithOAuth2Code(
    'shopify', completion.code, completion.codeVerifier, completion.redirectURL,
  );
}
```

Cet appel atteint `/api/collections/users/auth-with-oauth2`. Le fournisseur Go échange le vrai code Shopify, valide l'ID token et récupère le profil. La réponse d'authentification, les erreurs et les éventuels challenges MFA restent ceux de PocketBase. Le frontend doit traiter ces challenges s'il active la MFA.

Le SDK enregistre automatiquement le jeton et le compte dans son auth store. Son `LocalAuthStore` utilise le stockage du navigateur lorsqu'il est disponible. Les lectures, le renouvellement et le realtime passent ensuite par les API natives ; aucun appel de réhydratation via un cookie serveur n'est nécessaire. La déconnexion locale utilise `pb.authStore.clear()` et, si nécessaire, la fermeture des abonnements realtime du frontend. Elle ne déconnecte pas Shopify et ne révoque pas à elle seule les copies déjà émises du jeton PocketBase. [SDK officiel](https://github.com/pocketbase/js-sdk#auth-store), [authentification PocketBase](https://pocketbase.io/docs/authentication/).

## Sécurité et limites conservées

Les contrôles de state, navigateur, PKCE, nonce, signature JWKS, algorithme, émetteur, audience et expiration restent actifs. Les sujets Shopify numériques sont normalisés seulement après vérification de signature, sans conversion en flottant ni perte de précision.

Un e-mail identique ou un compte présent dans `Authorization` ne suffisent pas à fusionner une identité Shopify avec un compte existant. Seule une liaison d'identité exacte est acceptée ; un conflit renvoie HTTP 409. Les tokens Shopify ne sont pas exposés dans les métadonnées OAuth ou stockés pour une réutilisation ultérieure. Les sessions PocketBase et Shopify restent indépendantes.

Les transactions de connexion sont encore stockées en mémoire dans une instance Go. Un redémarrage interrompt les parcours en cours. Une exploitation multi-instance exige une stratégie explicite pour partager ou router ces transactions ; ce nettoyage ne l'implémente pas.

Le service expose uniquement le parcours `authWithOAuth2Code` avec redirection. La variante popup `authWithOAuth2` n'est pas prise en charge. Aucun contrôle périodique de la session Shopify, interface MFA, frontend ou synchronisation globale des déconnexions n'est ajouté.

Les exigences Customer Account API restent inchangées : nouveaux comptes clients activés, callback enregistré, scope `customer_read_customers` et accès requis aux données client protégées. Notre app existante et la boutique de test ne sont pas supprimées. [Authentification Shopify](https://shopify.dev/docs/api/customer/latest#authentication).

## Tests et historique

Voir [TESTS.md](TESTS.md) pour les contrôles du code actuel. Le fournisseur simulé est réservé aux tests Go ; il n'existe plus de commande lançant une fausse boutique ni de gateway publique de démonstration.

L'enquête initiale et les essais précédents sont conservés dans [l'historique du prototype](docs/prototype-history.md) et [l'historique des validations](docs/prototype-tests.md). Leurs anciennes routes et URL ne décrivent pas l'interface actuelle.
