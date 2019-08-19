/* global _, Trianglify, lunr, customElements, fetch, Math */
'use strict'

import 'https://unpkg.com/@webcomponents/webcomponentsjs/webcomponents-loader.js?module'
import {LitElement, html} from 'https://unpkg.com/@polymer/lit-element?module'
import {MDCTextField} from 'https://unpkg.com/@material/textfield?module'
import {MDCRipple} from 'https://unpkg.com/@material/ripple?module'
import {MDCLinearProgress} from 'https://unpkg.com/@material/linear-progress?module'
import 'https://unpkg.com/sanitize-html@1.20.1/dist/sanitize-html.min.js?module'

let endpoint = 'api/v1/recommendation'
// If '?prod' is appended to URL, point to prod.
if (window.location.search === '?prod') {
  endpoint = 'https://fn.lc/ficrecommend/' + endpoint
}

class MaterialButton extends LitElement {
  firstUpdated () {
    this.mdc = new MDCRipple(this.shadowRoot.querySelector('.mdc-button'))
  }

  render () {
    return html`
      <style>
        @import "https://unpkg.com/@material/button/dist/mdc.button.min.css";
        @import "static/shared.css";
      </style>

      <button class="mdc-button mdc-button--unelevated"><slot></slot></button>
    `
  }
}

customElements.define('material-button', MaterialButton)

class SafeHTML extends LitElement {
  static get properties () {
    return {
      html: { type: String }
    }
  }

  render () {
    return html`
      <div .innerHTML="${sanitizeHtml(this.html)}">
      </div>
    `
  }
}

customElements.define('safe-html', SafeHTML)

class MaterialProgress extends LitElement {
  firstUpdated () {
    this.mdc = new MDCLinearProgress(this.shadowRoot.querySelector('.mdc-linear-progress'))
  }

  render () {
    return html`
      <style>
        @import "https://unpkg.com/@material/linear-progress/dist/mdc.linear-progress.min.css";
      </style>

      <div role="progressbar" class="mdc-linear-progress mdc-linear-progress--indeterminate">
        <div class="mdc-linear-progress__buffering-dots"></div>
        <div class="mdc-linear-progress__buffer"></div>
        <div class="mdc-linear-progress__bar mdc-linear-progress__primary-bar">
          <span class="mdc-linear-progress__bar-inner"></span>
        </div>
        <div class="mdc-linear-progress__bar mdc-linear-progress__secondary-bar">
          <span class="mdc-linear-progress__bar-inner"></span>
        </div>
      </div>
    `
  }
}

customElements.define('material-progress', MaterialProgress)

class MaterialTextField extends LitElement {
  static get properties () {
    return {
      label: { type: String },
      type: { type: String, value: 'text' },
      value: { type: String, value: '' }
    }
  }

  firstUpdated (properties) {
    this.mdc = new MDCTextField(this.shadowRoot.querySelector('.mdc-text-field'))
  }

  render () {
    return html`
      <style>
        @import "https://unpkg.com/@material/textfield/dist/mdc.textfield.min.css";
        @import "static/shared.css";

        .mdc-text-field, input {
          width: 100%;
        }
        .mdc-text-field--focused:not(.mdc-text-field--disabled) .mdc-floating-label--float-above {
          color: var(--mdc-theme-primary);
        }
      </style>

      <div class="mdc-text-field mdc-text-field--outlined">
        <input type="${this.type}" id="tf-outlined" class="mdc-text-field__input" value="${this.value}">
        <div class="mdc-notched-outline">
          <div class="mdc-notched-outline__leading"></div>
          <div class="mdc-notched-outline__notch">
            <label for="tf-outlined" class="mdc-floating-label">${this.label}</label>
          </div>
          <div class="mdc-notched-outline__trailing"></div>
        </div>
      </div>
    `
  }
}

customElements.define('material-text-field', MaterialTextField)

class MaterialCard extends LitElement {
  render () {
    return html`
      <style>
        @import "https://unpkg.com/@material/card/dist/mdc.card.min.css";
        @import "static/shared.css";

        .mdc-card {
          padding: 16px;
          background-color: var(--background-color, white);
        }
      </style>

      <div class="mdc-card">
        <slot></slot>
      </div>
    `
  }
}

customElements.define('material-card', MaterialCard)

class StoryElement extends LitElement {
  static get properties () {
    return {
      story: { type: Object },
      score: { type: Number }
    }
  }

  roundTo (n, places) {
    const factor = 10 ** places
    return Math.round(n * factor) / factor
  }

  render () {
    const {story, score} = this
    const saveLink = 'http://ficsave.com/?format=epub&e=&auto_download=yes&story_url=' + story.url

    return html`
      <style>
        @import "static/shared.css";

        :host {
          display: block;
          background-color: #fff;
          line-height: 1.5rem;
          padding: 10px 15px;
          margin: 0;
          border-bottom: 1px solid #e0e0e0;
        }

        .stats {
          color: rgba(0,0,0,0.5);
          margin: 0;
        }
        .secondary-content {
          float: right;
        }
      </style>
      <a href="${story.url}" class="title">
        ${story.title}
        ${story.author ? ' by ' + story.author : ''}
      </a>
      ${score ? (' - ' + this.roundTo(score, 2)) : ''}
      <span class="secondary-content">
        <a href="${saveLink}">Download</a>,
        <a href="#/story/${story.url}">Search</a>
      </span>
      <div>
        <safe-html .html="${story.desc}"></safe-html>
      </div>
      <div class="stats">
        Chapters: ${story.chapters} -
        Reviews: ${story.reviews} -
        Likes: ${story.likecount} -
        Tags: ${(story.tags || []).slice(0, 25).join(', ')}
      </div>
    `
  }
}

customElements.define('story-element', StoryElement)

class OurgraphApp extends LitElement {
  static get properties () {
    return {
      stories: { type: Array },
      docs: { type: Array },
      curOffset: { type: Number },
      index: { type: Object },
      url: { type: String },
      filter: { type: String },
      error: { type: String },
      loading: { type: Boolean }
    }
  }

  constructor () {
    super()

    this.filter = ''
    this.url = ''
    this.stories = []
    this.goToPath(this.hashURL(), 0)

    window.addEventListener('hashchange', () => {
      this.goToPath(this.hashURL(), 0)
    })
  }

  render () {
    return html`
      <style>
        @import "static/shared.css";

        :host {
          --mdc-theme-primary: #039be5;
        }

        #stories {
          --background-color: rgba(255,255,255,0.6);
        }
        .error {
          color: red;
        }
        .header {
          text-align: center;
          margin: 80px 0 60px;
          font-size: 5.5rem;
          font-weight: 200;
          text-shadow: 0 2px 5px rgba(0,0,0,0.16),0 2px 10px rgba(0,0,0,0.12);
          color: white;
        }

        @media (max-width: 600px) {
          .row {
            margin-left: 0 !important;
            margin-right: 0 !important;
          }
          .header {
            font-size: 3.5rem !important;
          }
        }
        .small {
          width: 680px;
        }
        .big {
          width: 1024px;
        }
        .row {
          margin: 16px;
          display: flex;
          justify-content: center;
        }
        material-card {
          max-width: 100%;
        }
        h2 {
          margin: 0;
          font-size: 24px;
          font-weight: 300;
          display: block;
          line-height: 32px;
          margin-bottom: 8px;
          color: rgba(0,0,0,0.8);
        }
        .collection {
          border: 1px solid #e0e0e0;
          border-bottom: none;
          margin: 16px 0;
        }
        .row-space {
          display: flex;
          justify-content: space-between;
          align-items: center;
        }
        material-progress {
          margin-top: 16px;
        }
      </style>

      <div id="container">
        <div class="container">
          <h1 class="header">Ourgraph</h1>
          <div class="row">
            <material-card class="small">
              <h2>Recommend me something!</h2>
              <p>
              Ourgraph is a universal recommendation system for stories built on
              our user data spanning across multiple silos.
              </p>
              <div class="input-field">
                <material-text-field
                  label="Story URL"
                  type="url"
                  @input="${this.urlChanged}"
                  value="${this.url}">
                </material-text-field>
              </div>
              <p>
              For more focused results you can enter two URLs separated by a "|".
              </p>
              ${this.renderError()}
              <center>
                <material-button @click="${this.recommend}">
                  Recommend
                </material-button>
              </center>
              ${this.loading ? html`<material-progress></material-progress>` : null}
            </material-card>
          </div>

          ${this.renderStories()}

          <div class="row">
            <material-card class="small">
              <center>
                <a href="https://github.com/d4l3k/ourgraph">
                  <material-button>
                    Source Code
                  </material-button>
                </a>
              </center>
              <p>Copyright (c) 2019 <a href="https://fn.lc">Tristan Rice</a>.
              Licensed under the MIT license.</p>
            </material-card>
          </div>
        </div>
      </div>
    `
  }

  renderError () {
    if (this.error) {
      return html`
        <p class="error">${this.error}</p>
      `
    }
  }

  urlChanged (e) {
    this.url = e.originalTarget.value
    if (e.which === 13) {
      this.recommend()
    }
  }

  filterChanged (e) {
    this.filter = e.originalTarget.value
  }

  goToPath (path, offset) {
    const prefix = '/story/'
    if (path.indexOf(prefix) !== 0) {
      return
    }

    this.url = path.substr(prefix.length)

    if (!offset) {
      this.stories = []
      offset = 0
      this.filter = ''
      this.index = null
    }

    this.curOffset = offset

    if (this.url.length === 0) {
      return
    }

    this.loading = true

    fetch(endpoint + '?id=' + this.url + '&offset=' + offset)
      .then(resp => {
        if (resp.ok) {
          return resp.json()
        }

        return resp.text().then(error => {
          throw new Error(`${resp.status} ${resp.statusText}: ${error}`)
        })
      })
      .then(data => {
        console.log(data)

        this.stories = this.stories.concat(data.Recommendations)
        const stories = this.stories
        this.index = lunr(function () {
          this.ref('id')
          this.field('title')
          this.field('desc')
          this.field('tags')

          stories.forEach((story, i) => {
            this.add({
              id: i,
              ...story.Document
            })
          })
        })

        this.data = data
        this.error = null
      }).catch(err => {
        console.error(err)
        this.error = err
      }).then(() => {
        this.loading = false
      })
  }

  renderStories () {
    if (this.stories.length === 0) {
      return
    }

    const query = this.filter
    let dispStories = []
    if (query.length === 0) {
      dispStories = this.stories
    } else {
      this.index.search(query).forEach(result => {
        dispStories.push(this.stories[result.ref])
      })
    }
    const out = []
    dispStories.forEach(story => {
      out.push(html`<story-element .story=${story.Document} .score=${story.Score}>`)
    })

    return html`
      <div class="row">
        <material-card class="big" id="stories">
          <h2>Input</h2>
          <div class="collection">
            ${this.data.Documents.map(doc => html`<story-element .story=${doc}>`)}
          </div>

          <div class="row-space">
            <h2>
              Recommended Stories
            </h2>
            <material-text-field
              label="Filter"
              type="text"
              @input="${this.filterChanged}"
              value="${this.filter}">
            </material-text-field>
          </div>

          <div class="collection">
            ${out}
          </div>
          <center>
            <material-button @click="${this.more}">
              More Stories
            </material-button>
          </center>
          ${this.loading ? html`<material-progress></material-progress>` : null}
        </material-card>
      </div>
    `
  }

  hashURL () {
    return window.location.hash.substr(1)
  }

  more () {
    this.curOffset += 100
    this.goToPath(this.hashURL(), this.curOffset)
  }

  recommend () {
    const newPath = '/story/' + this.url
    window.location.hash = '#' + newPath
    this.goToPath(newPath)
  }
}

customElements.define('ourgraph-app', OurgraphApp)

{
  const background = document.getElementById('background')

  function renderBackground () {
    const pattern = Trianglify({
      width: window.innerWidth,
      height: window.innerHeight
    })
    pattern.canvas(background)
  }

  let timeout
  window.addEventListener('resize', function () {
    clearTimeout(timeout)
    timeout = setTimeout(function () {
      renderBackground()
    }, 300)
  })
  window.addEventListener('hashchange', function () {
    renderBackground()
  })

  renderBackground()
}
