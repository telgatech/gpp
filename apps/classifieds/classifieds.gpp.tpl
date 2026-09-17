package main

template Home(data HomePage) {
<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Classifieds</title>
    <style>
        :root { color-scheme: light; font-family: system-ui, sans-serif; }
        body { margin: 0; background: #f5f6f8; color: #20242b; }
        header { background: #17202c; color: white; padding: 1rem max(1rem, calc((100% - 70rem) / 2)); }
        main { max-width: 70rem; margin: 0 auto; padding: 2rem 1rem; }
        nav { display: flex; justify-content: space-between; align-items: center; gap: 1rem; }
        nav a { color: white; text-decoration: none; font-weight: 700; }
        form.search { display: flex; gap: .5rem; margin: 1.5rem 0; }
        input, textarea, select { box-sizing: border-box; width: 100%; padding: .7rem; border: 1px solid #c9ced6; border-radius: .4rem; background: white; }
        button, .button { border: 0; border-radius: .4rem; padding: .7rem 1rem; background: #2266d1; color: white; text-decoration: none; cursor: pointer; }
        .button.secondary { background: #667085; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(18rem, 1fr)); gap: 1rem; }
        .card { background: white; border: 1px solid #e1e5eb; border-radius: .6rem; padding: 1.2rem; box-shadow: 0 1px 2px #0000000d; }
        .price { font-size: 1.3rem; font-weight: 800; }
        .muted { color: #667085; }
        .empty { padding: 3rem 1rem; text-align: center; background: white; border-radius: .6rem; }
        @media (max-width: 32rem) { form.search { flex-direction: column; } }
    </style>
</head>
<body>
<header><nav><a href="/">Classifieds</a><a href="/listings/new">Post an item</a></nav></header>
<main>
    <h1>Find something useful</h1>
    <form class="search" method="get" action="/">
        <input name="q" value="{{.Query}}" placeholder="Search listings">
        <button type="submit">Search</button>
    </form>
    {{if .Listings}}
    <div class="grid">
        {{range .Listings}}
        <article class="card">
            <h2><a href="/listings/{{.Id}}">{{.Title}}</a></h2>
            <p class="price">${{printf "%.2f" .Price}}</p>
            <p>{{.Description}}</p>
            <p class="muted">{{.Location}} · {{.CreatedAt}}</p>
        </article>
        {{end}}
    </div>
    {{else}}
    <div class="empty"><p>No active listings matched your search.</p><a class="button" href="/listings/new">Post the first listing</a></div>
    {{end}}
</main>
</body>
</html>
}

template ListingDetail(data ListingPage) {
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Listing.Title}} · Classifieds</title></head>
<body><main>
    <p><a href="/">← All listings</a></p>
    <article>
        <h1>{{.Listing.Title}}</h1>
        <p><strong>${{printf "%.2f" .Listing.Price}}</strong> · {{.Listing.Location}}</p>
        <p>{{.Listing.Description}}</p>
        <p>Seller: {{.Seller.Name}} ({{.Seller.Email}})</p>
        <p><a href="/listings/{{.Listing.Id}}/edit">Edit listing</a></p>
        <form method="post" action="/listings/{{.Listing.Id}}/delete"><button type="submit">Delete listing</button></form>
    </article>
</main></body></html>
}

template NewListing(data ListingFormPage) {
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Post listing · Classifieds</title></head>
<body><main>
    <p><a href="/">← All listings</a></p><h1>Post a listing</h1>
    {{if .Error}}<p style="color:#b42318">{{.Error}}</p>{{end}}
    <form method="post" action="/listings">
        <p><label>Title<br><input name="title" required maxlength="120"></label></p>
        <p><label>Description<br><textarea name="description" rows="6"></textarea></label></p>
        <p><label>Price<br><input name="price" type="number" min="0" step="0.01" required></label></p>
        <p><label>Location<br><input name="location" required></label></p>
        <p><label>Category<br><select name="category_id" required>{{range .Categories}}<option value="{{.Id}}">{{.Name}}</option>{{end}}</select></label></p>
        <button type="submit">Publish listing</button>
    </form>
</main></body></html>
}

template EditListing(data ListingFormPage) {
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Edit listing · Classifieds</title></head>
<body><main>
    <p><a href="/listings/{{.Listing.Id}}">← Listing</a></p><h1>Edit listing</h1>
    {{if .Error}}<p style="color:#b42318">{{.Error}}</p>{{end}}
    <form method="post" action="/listings/{{.Listing.Id}}/edit">
        <p><label>Title<br><input name="title" value="{{.Listing.Title}}" required maxlength="120"></label></p>
        <p><label>Description<br><textarea name="description" rows="6">{{.Listing.Description}}</textarea></label></p>
        <p><label>Price<br><input name="price" value="{{printf "%.2f" .Listing.Price}}" type="number" min="0" step="0.01" required></label></p>
        <p><label>Location<br><input name="location" value="{{.Listing.Location}}" required></label></p>
        <p><label>Category<br><select name="category_id" required>{{range .Categories}}<option value="{{.Id}}" {{if eq .Id $.Listing.CategoryId}}selected{{end}}>{{.Name}}</option>{{end}}</select></label></p>
        <button type="submit">Save changes</button>
    </form>
</main></body></html>
}

template ErrorPage(data any) {
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Code}} {{.Message}}</title></head>
<body><main><h1>{{.Code}} {{.Message}}</h1><p>The request could not be completed.</p><p><a href="/">Return to classifieds</a></p><p class="muted">{{.Path}}</p></main></body></html>
}
