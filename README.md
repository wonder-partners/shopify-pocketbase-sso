# Authentification Shopify dans PocketBase

L'intégration ajoute un fournisseur `shopify` au parcours OAuth2 natif de PocketBase. Le frontend utilise `listAuthMethods()`, redirige vers Shopify, vérifie le retour puis appelle `authWithOAuth2Code()`. Le SDK reçoit le token PocketBase et met son auth store à jour.

Le module Go `studio-sso` et le package `internal/studio` restent en place. Aucun packaging ni publication n'est préparé.

## Développement avec mise

Le projet utilise [mise-en-place](https://mise.jdx.dev/) pour Go `1.27.2`, Node `24.14.1`, cloudflared `2026.10.1`, les variables locales et les tâches. npm est fourni avec Node. Les versions sont fixées dans `mise.toml`. Les outils sont installés dans le stockage de mise, sans copie dans le dépôt. `bin/` est réservé aux résultats de compilation de l'application ; il contient aussi d'anciens builds du prototype conservés pour l'historique local.

Après installation de mise, depuis la racine du dépôt :

```powershell
mise trust
mise install
mise run setup
```

Les variables sont directement dans `[env]` de `mise.toml` : URL de boutique, URL de callback, Client ID public et exception HTTP loopback. L'intégration utilise exclusivement un client public PKCE, sans secret client à demander ou configurer. Aucun fichier local ou fichier d'exemple n'est nécessaire pour ce parcours.

mise charge automatiquement ces fichiers avec `mise run` et `mise exec`, sans script PowerShell à sourcer. L'ancien `config.local.ps1` a été retiré après migration.

| Commande | Effet |
| --- | --- |
| `mise run setup` | Télécharge les modules Go et installe le SDK fixé pour les tests |
| `mise run dev` | Lance PocketBase et le frontend sur `127.0.0.1:8090` |
| `mise run tunnel` | Ouvre un tunnel HTTPS temporaire vers le serveur local |
| `mise run test` | Prépare les dépendances et lance toute la suite, avec les trois intégrations SDK |
| `mise run build` | Compile le binaire dans `bin/` |
| `mise run check` | Lance tests, analyse Go, build, contrôles JavaScript et diff Git |

`mise tasks` liste aussi les tâches individuelles `vet`, `check:js` et `check:diff`. Les tâches partent de la racine du projet et ne nécessitent pas d'activer mise dans le profil du shell. Les tests utilisent leurs propres serveurs et bases temporaires ; ils n'exigent pas de configuration Shopify locale. `mise.local.toml`, ignoré par Git, reste utilisable pour d'éventuelles surcharges personnelles.

## Responsabilités

- PocketBase génère le state, le verifier et le challenge PKCE via son endpoint natif `users/auth-methods`.
- Le frontend conserve ces paramètres dans le `sessionStorage` de l'onglet, vérifie le state et son expiration au retour, puis les supprime avant tout échange.
- Le fournisseur Go échange le vrai code Shopify, valide l'ID token et récupère l'identifiant client et l'e-mail.
- PocketBase crée ou retrouve le compte `users`, conserve la liaison dans `_externalAuths` et applique ses règles, hooks, MFA et enrichissements natifs.

Il n'existe plus de `/api/shopify/auth/start`, de `/api/shopify/auth/complete`, de cookie OAuth, ni de transactions de connexion stockées en mémoire côté serveur. Le package `internal/studio` ne fournit aucune page HTML ni API de session parallèle. L'hôte sert désormais une page de test minimale dans `pb_public`.

Le state est désormais vérifié par le frontend, pas par le backend. La possession du verifier PKCE protège l'échange du code. Les codes à usage unique et leur expiration sont contrôlés par Shopify. Le délai de cinq minutes dans l'exemple est un contrôle local supplémentaire, pas une expiration imposée par notre serveur.

## Configuration et hôte Go

`main.go` reste un hôte PocketBase minimal. Il initialise `studio.New` dans `OnServe`, puis appelle `Register`. Ce dernier ne déclare aucune route : il adapte uniquement les requêtes OAuth natives.

L'hôte enregistre aussi `apis.Static` pour servir `pb_public`, sans fallback HTML global. Seul `/auth/callback` sert explicitement la page d'accueil pour traiter le retour OAuth. Les préfixes API, admin et les anciens chemins `/studio` ne sont pas servis par le frontend. Le service statique reste dans `main.go`.

## Page de test locale

`pb_public/index.html` contient tout le CSS et le JavaScript de la page. Le SDK officiel est importé depuis jsDelivr, fixé à `0.28.1`. Aucune copie du SDK n'est conservée dans `pb_public`. La CSP autorise ce fichier CDN précis et les empreintes SHA-256 des blocs inline. Si ces blocs changent, mettre à jour leurs empreintes dans `main.go` ; les tests signalent un décalage. Le navigateur doit pouvoir joindre ce CDN ; les tests Node utilisent la même version installée dans `integration`.

La page reprend le flux [Manual code exchange de PocketBase](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2), avec les deux fonctions testées ci-dessous. Une tentative silencieuse est faite à l'arrivée sans session locale. Le callback ne la relance jamais automatiquement. Les boutons permettent une connexion interactive, un test silencieux explicite, la vérification de l'API et la déconnexion locale. Après connexion, la même instance du SDK appelle `authRefresh()` puis `getOne()` sur son propre compte. Après succès, l'adresse revient sur `/` pour permettre le rechargement avec l'auth store restauré.

Les erreurs attendues de `prompt=none` proposent le bouton de connexion. Une MFA est signalée, sans interface de second facteur. Les tokens, codes OAuth, verifiers et identifiants Shopify bruts ne sont pas affichés. Le callback est servi en `no-store`, avec `Referrer-Policy: no-referrer` et une CSP adaptée au CDN.

Lancement depuis la racine du dépôt, lorsque les URL de test sont alignées :

```powershell
mise run dev
```

`--publicDir` vaut `./pb_public` par défaut, relatif au répertoire de travail, y compris avec un binaire placé dans `bin`. Utiliser un chemin absolu évite toute ambiguïté.

La tâche `dev` fournit les chemins absolus de `pb_data` et `pb_public` à partir de la racine du projet.

Dans un second terminal à la racine du projet, `mise run tunnel` lance le cloudflared géré par mise. Reporter l'origine HTTPS affichée dans `SHOPIFY_REDIRECT_URL`, avec `/auth/callback`, et enregistrer cette URL exacte dans Shopify avant le login réel. Redémarrer le serveur après modification de sa configuration. Le tunnel direct publie également les routes admin de PocketBase ; l'arrêter après le test.

Le frontend utilise l'origine courante et `/auth/callback`. Avant un login réel, aligner exactement cette URL, `SHOPIFY_REDIRECT_URL` et `[customer_authentication].redirect_uris`. La configuration locale et l'app Shopify utilisent encore `/studio/callback` ; cette étape n'a modifié ni l'une ni l'autre et n'a pas démarré le backend sur les données locales réelles.

## Configuration du fournisseur

| Paramètre Go | Variable d'environnement | Rôle |
| --- | --- | --- |
| `ShopURL` | `SHOPIFY_SHOP_URL` | Origine HTTPS de la boutique de confiance |
| `RedirectURL` | `SHOPIFY_REDIRECT_URL` | Callback du frontend, enregistré à l'identique dans Shopify |
| `ClientID` | `SHOPIFY_CUSTOMER_CLIENT_ID` | Client Customer Account API |
| `AllowLoopbackHTTP` | `SSO_ALLOW_LOOPBACK_HTTP=1` | Exception de développement limitée au loopback |

Le POST OAuth exige l'en-tête `Origin` correspondant à l'origine de `RedirectURL`, que le navigateur envoie automatiquement. Cette origine est celle du frontend, pas nécessairement celle de l'API. Si le frontend et l'API ont des origines différentes, leur configuration CORS doit autoriser les requêtes nécessaires. Aucun cookie ni en-tête manuel n'est requis côté navigateur. Un proxy doit transmettre l'en-tête `Origin` sans le réécrire.

L'application hôte choisit et sert sa page de callback. Aucun chemin n'est imposé ou enregistré par le module Go. `/auth/callback` dans l'exemple ci-dessous est une convention d'illustration, pas une route fournie par la bibliothèque. L'URL complète choisie doit être identique dans le frontend, dans `RedirectURL` et dans la configuration Shopify.

La collection d'auth est fixée à `users` et doit exister. L'intégration n'y ajoute aucun champ et ne choisit aucune règle d'accès. Elle active OAuth2 et configure uniquement `shopify`, sans modifier les autres fournisseurs, le mot de passe, l'OTP, la MFA ou la durée des tokens.

`_shopifyIdentities` est une collection système avec les champs `user`, `issuer`, `subject` et `customerId`. Ses cinq règles API sont verrouillées. Les identifiants bruts sont réservés au serveur et aux superusers. Les prénoms et noms ne sont ni demandés ni synchronisés.

## Exemple frontend minimal

Dans un frontend avec bundler et le SDK officiel installé :

```javascript
import PocketBase from 'pocketbase';

const pb = new PocketBase(window.location.origin);
// Chemin choisi et servi par l'application hôte, pas par le module Go.
// L'URL complète doit correspondre à SHOPIFY_REDIRECT_URL et au callback Shopify.
const redirectURL = new URL('/auth/callback', window.location.origin).href;
```

Les deux fonctions suivantes suffisent pour le passage OAuth. Elles appartiennent au frontend hôte ; aucun helper JavaScript n'est ajouté au backend Go.

<!-- native-redirect-example:start -->
```javascript
async function loginWithShopify(silent = false) {
  const methods = await pb.collection('users').listAuthMethods();
  const provider = methods.oauth2.providers.find(p => p.name === 'shopify');
  if (!provider) throw new Error('Shopify non configuré.');

  sessionStorage.setItem('shopify.oauth', JSON.stringify({
    state: provider.state,
    verifier: provider.codeVerifier,
    expires: Date.now() + 5 * 60 * 1000,
  }));

  const url = new URL(provider.authURL + encodeURIComponent(redirectURL));
  if (silent) url.searchParams.set('prompt', 'none');
  location.assign(url.href);
}

async function completeShopifyLogin() {
  const params = new URLSearchParams(location.search);
  history.replaceState(null, '', location.pathname);

  const saved = sessionStorage.getItem('shopify.oauth');
  sessionStorage.removeItem('shopify.oauth');
  const flow = JSON.parse(saved || 'null');

  if (!flow || !Number.isFinite(flow.expires) ||
      flow.expires <= Date.now() || flow.state !== params.get('state')) {
    throw new Error('Retour Shopify invalide ou expiré.');
  }

  if (params.has('error')) return { error: params.get('error') };
  const code = params.get('code');
  if (!code) throw new Error('Code Shopify manquant.');

  // Empêcher la liaison avec un autre compte déjà connecté.
  pb.authStore.clear();
  return pb.collection('users').authWithOAuth2Code(
    'shopify', code, flow.verifier, redirectURL,
  );
}
```
<!-- native-redirect-example:end -->

Utilisation :

- À l'arrivée sans compte PocketBase connecté : appeler `loginWithShopify(true)` une seule fois.
- Au clic sur le bouton de connexion : appeler `loginWithShopify(false)`.
- Sur le callback : appeler `completeShopifyLogin()`. En cas de `login_required`, `interaction_required` ou `consent_required`, afficher le bouton. En cas de `access_denied`, afficher l'annulation. Ne jamais relancer automatiquement une tentative silencieuse depuis le callback.
- Gérer les erreurs des promesses dans l'interface. Ne pas afficher directement des paramètres OAuth arbitraires, ni journaliser le code, le verifier ou les tokens.
- Après un succès, le SDK a déjà alimenté `pb.authStore`. Les appels CRUD, le refresh et le realtime utilisent l'authentification native.

Le frontend doit servir le callback avec `Cache-Control: no-store` et `Referrer-Policy: no-referrer`, sans analytics, avec le seul SDK externe fixé depuis le CDN. Il doit effacer le code de l'adresse avant tout traitement. Le `sessionStorage` contient temporairement un secret PKCE : protéger ce frontend contre les injections de script est indispensable.

Le code de cet exemple est exécuté tel quel par le test d'intégration avec le SDK officiel. Les mêmes fonctions sont présentes dans `pb_public/index.html` ; un test vérifie leur parité.

## Sécurité et limites

Le fournisseur ajoute automatiquement un nonce à l'URL native : le nonce est le challenge S256 PKCE, jamais le verifier secret. C'est une valeur imprévisible et différente pour chaque tentative. Au moment de l'échange, le serveur recalcule le challenge depuis le verifier reçu et compare le nonce de l'ID token signé. Aucune valeur attendue n'a besoin d'être conservée côté serveur.

Le serveur conserve les contrôles de format PKCE, callback exact, client, origine, signature JWKS, algorithme, issuer, audience, expiration, nonce et `at_hash` si présent. Les sujets Shopify numériques sont normalisés après vérification de signature, sans perte de précision. Le POST natif et la réponse des méthodes d'auth sont marqués `no-store`.

PocketBase impose un champ `ClientSecret` non vide dans sa configuration de fournisseur. Le marqueur public interne `public-pkce-no-secret` satisfait uniquement cette validation. Il n'est jamais envoyé à Shopify : le client OAuth d'échange ne possède aucun secret, et le simulateur refuse tout paramètre `client_secret` ou en-tête d'authentification du client.

Un e-mail identique ou un compte présent dans `Authorization` ne suffisent pas à fusionner une identité Shopify avec un compte existant. Seule une liaison exacte est acceptée ; un conflit renvoie HTTP 409. Les tokens Shopify ne sont ni exposés dans les métadonnées OAuth ni conservés pour des appels ultérieurs.

La mémoire tampon de réponse OAuth empêche la fuite d'un JWT lorsqu'un hook refuse après `e.Next()`. Elle ne fabrique aucun token et conserve les réponses natives, notamment les challenges MFA. Le frontend doit gérer une MFA si le projet l'active ; l'exemple ci-dessus ne fournit pas cet écran.

Le parcours documenté et testé est la redirection manuelle avec `authWithOAuth2Code`. La popup `authWithOAuth2` n'est pas activée par cette étape, car elle utilise un autre callback. Aucun iframe silencieux, contrôle périodique de session Shopify ni synchronisation globale des déconnexions n'est ajouté.

Les sessions PocketBase et Shopify restent indépendantes. `pb.authStore.clear()` déconnecte localement le studio ; il ne déconnecte pas Shopify et ne révoque pas les copies déjà émises du token. Une nouvelle tentative silencieuse peut reconnecter le client si Shopify est toujours connecté.

L'intégration ne conserve plus de transaction OAuth locale qui impose de rester sur la même instance Go. Cela ne configure pas pour autant une infrastructure PocketBase multi-instance ou un stockage partagé.

[Parcours OAuth PocketBase](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2), [SDK et auth store](https://github.com/pocketbase/js-sdk#auth-store), [authentification Shopify](https://shopify.dev/docs/api/customer/2026-10#authentication).

## Tests et historique

Voir [TESTS.md](TESTS.md). Le fournisseur simulé est réservé aux tests ; aucune fausse boutique ni gateway n'est distribuée.

Les migrations historiques de `migrations` restent testées mais ne sont pas importées par `main.go` ou le cœur. Ne pas réutiliser leurs anciens nettoyages sur un autre projet. Ce changement ne migre ni les comptes ni les liaisons Shopify existantes.

L'enquête et les validations antérieures restent dans [docs/prototype-history.md](docs/prototype-history.md) et [docs/prototype-tests.md](docs/prototype-tests.md). Leurs anciens parcours et URL ne décrivent pas l'interface actuelle.
