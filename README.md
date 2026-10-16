# Authentification Shopify dans PocketBase

Le package Go `shopifysso` ajoute un fournisseur `shopify` au parcours OAuth2 natif de PocketBase. Le frontend utilise `listAuthMethods()`, redirige vers Shopify puis appelle `authWithOAuth2Code()` après vérification du retour. PocketBase gère ensuite ses tokens, règles, hooks et MFA.

Ce projet contient la bibliothèque, ses tests et sa documentation. L’application hôte possède sa collection d’authentification, ses champs, ses règles, son frontend et son callback. La bibliothèque configure le fournisseur `shopify` et son stockage privé d’identités.

`New` découvre les endpoints distants et configure la base. `Register` adapte les requêtes OAuth natives. Le package ne démarre aucun serveur, ne sert aucune page et ne lit aucune variable d’environnement.

`internal/mockshop` et `tests/sdk` servent uniquement aux tests de la bibliothèque. Le code de production ne les importe pas.

## Installation dans un projet Go PocketBase

Le chemin du module est `github.com/Sov3rain/shopify-pocketbase-sso`. Cette extraction est locale, sans version publiée à cette étape. Après publication d’un commit ou d’une version, l’installation se fera avec :

```sh
go get github.com/Sov3rain/shopify-pocketbase-sso@<version-ou-commit>
```

Pour utiliser le checkout actuel depuis un autre module, ajouter temporairement un `replace` vers son chemin local, puis demander la dépendance :

```sh
go mod edit -replace=github.com/Sov3rain/shopify-pocketbase-sso=/chemin/vers/shopify-pocketbase-sso
go get github.com/Sov3rain/shopify-pocketbase-sso
```

Initialiser une fois le service dans `OnServe`, après le bootstrap et les migrations du projet hôte :

```go
package main

import (
    "context"
    "log"

    shopifysso "github.com/Sov3rain/shopify-pocketbase-sso"
    "github.com/pocketbase/pocketbase"
    "github.com/pocketbase/pocketbase/core"
)

func main() {
    app := pocketbase.New()
    app.OnServe().BindFunc(func(e *core.ServeEvent) error {
        sso, err := shopifysso.New(context.Background(), app, shopifysso.Config{
            ShopURL: "https://ma-boutique.myshopify.com",
            RedirectURL: "https://mon-frontend.example/redirect.html",
            ClientID: "mon-client-public",
            AuthCollection: "users",
        })
        if err != nil {
            return err
        }
        sso.Register(e.Router)
        return e.Next()
    })
    if err := app.Start(); err != nil {
        log.Fatal(err)
    }
}
```

Le frontend doit servir son propre callback. Aucun frontend, tunnel, outil Node ou migration du prototype n’est nécessaire pour importer et compiler la bibliothèque.

La compatibilité est actuellement testée avec PocketBase `0.40.5` et Go `1.27.2`, fixés dans `go.mod`. Le périmètre est une boutique et une collection d’auth par application PocketBase. Plusieurs instances PocketBase peuvent coexister dans le même processus, chacune avec son service. Changer de collection après la création des identités nécessite une migration du projet hôte. Aucun déplacement de comptes n’est automatique.

## Configuration du fournisseur

| Paramètre Go | Rôle |
| --- | --- |
| `ShopURL` | Origine HTTPS de la boutique de confiance |
| `RedirectURL` | Callback du frontend, enregistré à l’identique dans Shopify |
| `ClientID` | Client public Customer Account API |
| `AuthCollection` | Nom ou ID d’une collection d’auth existante, `users` par défaut |
| `AllowLoopbackHTTP` | Exception de développement limitée au loopback |

Le POST OAuth exige l'en-tête `Origin` correspondant à l'origine de `RedirectURL`, que le navigateur envoie automatiquement. Cette origine est celle du frontend, pas nécessairement celle de l'API. Si le frontend et l'API ont des origines différentes, leur configuration CORS doit autoriser les requêtes nécessaires. Aucun cookie ni en-tête manuel n'est requis côté navigateur. Un proxy doit transmettre l'en-tête `Origin` sans le réécrire.

L'application hôte choisit et sert sa page de callback. Aucun chemin n'est imposé ou enregistré par le module Go. `/redirect.html` dans l'exemple ci-dessous est une convention d'illustration, pas une route fournie par la bibliothèque. L'URL complète choisie doit être identique dans le frontend, dans `RedirectURL` et dans la configuration Shopify.

La collection d’auth choisie par `AuthCollection` doit exister. Les collections ordinaires et `_superusers` sont refusées. L'intégration n'y ajoute aucun champ et ne choisit aucune règle d'accès. Elle active OAuth2 et configure uniquement `shopify`, sans modifier les autres fournisseurs, le mot de passe, l'OTP, la MFA ou la durée des tokens. La règle de création du projet hôte décide si une première connexion peut créer un compte.

`_shopifyIdentities` est une collection système avec les champs `user`, `issuer`, `subject` et `customerId`. Ses cinq règles API sont verrouillées. Les identifiants bruts sont réservés au serveur et aux superusers. Les prénoms et noms ne sont ni demandés ni synchronisés.

## Exemple frontend minimal

L’application hôte gère la navigation vers Shopify et le retour OAuth. Le callback choisi doit vérifier le state, supprimer les paramètres temporaires et échanger le code avec le SDK officiel.

Adapter `pb.collection('users')` au nom de la collection configurée si elle diffère. Dans un frontend avec bundler et le SDK officiel installé :

```javascript
import PocketBase from 'pocketbase';

const pb = new PocketBase(window.location.origin);
// Chemin choisi et servi par l'application hôte, pas par le module Go.
// L’URL complète doit correspondre à Config.RedirectURL et au callback Shopify.
const redirectURL = new URL('/redirect.html', window.location.origin).href;
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
- Sur le callback : appeler `completeShopifyLogin()`. En cas de `login_required`, `interaction_required` ou `consent_required`, proposer un retour aux boutons de connexion. En cas de `access_denied`, afficher l'annulation. Ne jamais relancer automatiquement une tentative silencieuse depuis le callback.
- Gérer les erreurs des promesses dans l'interface. Ne pas afficher directement des paramètres OAuth arbitraires, ni journaliser le code, le verifier ou les tokens.
- Après un succès, le SDK a déjà alimenté `pb.authStore`. Les appels CRUD, le refresh et le realtime utilisent l'authentification native.

Le frontend doit servir le callback avec `Cache-Control: no-store` et `Referrer-Policy: no-referrer`, sans analytics et avec des scripts autorisés explicitement. Il doit effacer le code de l'adresse avant tout traitement. Le `sessionStorage` contient temporairement un secret PKCE : protéger ce frontend contre les injections de script est indispensable.

Le code de cet exemple est exécuté tel quel par le test d’intégration de la bibliothèque avec le SDK officiel.

## Sécurité et limites

Le fournisseur ajoute automatiquement un nonce à l'URL native : le nonce est le challenge S256 PKCE, jamais le verifier secret. C'est une valeur imprévisible et différente pour chaque tentative. Au moment de l'échange, le serveur recalcule le challenge depuis le verifier reçu et compare le nonce de l'ID token signé. Aucune valeur attendue n'a besoin d'être conservée côté serveur.

Le serveur conserve les contrôles de format PKCE, callback exact, client, origine, signature JWKS, algorithme, issuer, audience, expiration, nonce et `at_hash` si présent. Les sujets Shopify numériques sont normalisés après vérification de signature, sans perte de précision. Le POST natif et la réponse des méthodes d'auth sont marqués `no-store`.

PocketBase impose un champ `ClientSecret` non vide dans sa configuration de fournisseur. Le marqueur public interne `public-pkce-no-secret` satisfait uniquement cette validation. Il n'est jamais envoyé à Shopify : le client OAuth d'échange ne possède aucun secret, et le simulateur refuse tout paramètre `client_secret` ou en-tête d'authentification du client.

Un e-mail identique ou un compte présent dans `Authorization` ne suffisent pas à fusionner une identité Shopify avec un compte existant. Seule une liaison exacte est acceptée ; un conflit renvoie HTTP 409. Les tokens Shopify ne sont ni exposés dans les métadonnées OAuth ni conservés pour des appels ultérieurs.

La mémoire tampon de réponse OAuth empêche la fuite d'un JWT lorsqu'un hook refuse après `e.Next()`. Elle ne fabrique aucun token et conserve les réponses natives, notamment les challenges MFA. Le frontend doit gérer une MFA si le projet l'active ; l'exemple ci-dessus ne fournit pas cet écran.

Le parcours documenté et testé est la redirection manuelle avec `authWithOAuth2Code`. La popup `authWithOAuth2` n'est pas activée par cette étape, car elle utilise un autre callback. Aucun iframe silencieux, contrôle périodique de session Shopify ni synchronisation globale des déconnexions n'est ajouté.

Les sessions PocketBase et Shopify restent indépendantes. `pb.authStore.clear()` déconnecte localement l’application ; il ne déconnecte pas Shopify et ne révoque pas les copies déjà émises du token. Une nouvelle tentative silencieuse peut reconnecter le client si Shopify est toujours connecté.

L'intégration ne conserve plus de transaction OAuth locale qui impose de rester sur la même instance Go. Cela ne configure pas pour autant une infrastructure PocketBase multi-instance ou un stockage partagé.

[Parcours OAuth PocketBase](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2), [SDK et auth store](https://github.com/pocketbase/js-sdk#auth-store), [authentification Shopify](https://shopify.dev/docs/api/customer/2026-10#authentication).

## Tests

Voir le [guide des tests](tests/README.md). Le fournisseur simulé et le SDK JavaScript sont réservés aux tests.

L’application de démonstration est un [projet séparé dans le dossier voisin](../shopify-pocketbase-sso-demo/README.md).
