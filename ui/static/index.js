/* global _, Trianglify, lunr */

'use strict'

var background = document.getElementById('background')
var endpoint = 'api/v1/recommendation'

const $progress = document.querySelector('.progress')

// If '?prod' is appended to URL, point to prod.
if (window.location.search === '?prod') {
  endpoint = 'https://fn.lc/ficrecommend/' + endpoint
}
function renderBackground () {
  var pattern = Trianglify({
    width: window.innerWidth,
    height: window.innerHeight * 4
  })
  pattern.canvas(background)
};
renderBackground()

function storyElement (story, score) {
  var saveLink = 'http://ficsave.com/?format=epub&e=&auto_download=yes&story_url=' + story.url
  return `<li class="collection-item">
    <a href="${story.url}" class="title">${story.title}</a>
    ${(score ? (' - ' + score) : '')}
    <span class="secondary-content">
      <a href="${saveLink}">Download</a>,
      <a href="#/story/${story.url}">Search</a>
    </span>
    <div>
      ${story.desc}
    </div>
  </li>`
}
function hide (elem) {
  elem.classList.add('hidden')
}
function show (elem) {
  elem.classList.remove('hidden')
}
var curOffset = 0
var stories = []
var index

function goToPath (path, offset) {
  if (_.startsWith(path, '/story/')) {
    var stationId = _.trimStart(path, '/story/')
    document.querySelector('#url').value = stationId

    var $error = document.querySelector('.error')
    var $filter = document.querySelector('#filter')

    if (!offset) {
      hide(document.querySelector('#stories'))
      stories = []
      offset = 0
      $filter.value = ''
      index = null
    }
    hide($error)

    curOffset = offset

    if (stationId.length == 0) {
      hide($progress)
      return
    }
    show($progress)

    fetch(endpoint + '?callback=?&id=' + stationId + '&offset=' + offset)
      .then(resp => resp.json())
      .then(data => {
        stories = stories.concat(data.Recommendations)
        index = lunr(function () {
          this.ref('id')
          this.field('title')
          this.field('desc')

          stories.forEach((story, i) => {
            this.add({
              id: i,
              ...story.Document
            })
          })
        })

        const docHtml = data.Documents.map(v => storyElement(v)).join('\n')
        console.log(docHtml)
        document.querySelector('#input-col').innerHTML = docHtml
        renderStories()
        show(document.querySelector('#stories'))
      }).catch(err => {
        console.log(err)
        $error.innerText = err
        show($error)
      }).then(() => {
        hide($progress)
      })
  }
  renderBackground()
}

function renderStories () {
  var $filter = document.querySelector('#filter')
  var query = $filter.value.trim()
  var dispStories = []
  if (query.length === 0) {
    dispStories = stories
  } else {
    var searchResults = index.search(query)
    console.log(query, searchResults)
    _.each(searchResults, function (result) {
      dispStories.push(stories[result.ref])
    })
  }
  var html = ''
  _.each(dispStories, function (story) {
    html += storyElement(story.Document, story.Score)
  })
  document.querySelector('#stories-col').innerHTML = html
}

document.querySelector('#filter').addEventListener('keydown', _.debounce(renderStories, 300))

function more () {
  curOffset += 100
  goToPath(window.location.hash.substr(1), curOffset)
}

function recommend () {
  var val = document.querySelector('#url').value
  var newPath = '/story/' + val
  window.location.hash = '#' + newPath
  goToPath(newPath)
}

document.querySelector('#rec').addEventListener('click', recommend)

var timeout
window.addEventListener('resize', function () {
  clearTimeout(timeout)
  timeout = setTimeout(function () {
    renderBackground()
  }, 300)
})

document.querySelector('#more').addEventListener('click', function () {
  more()
})

document.querySelector('#url').addEventListener('keypress', function (e) {
  if (e.which === 13) {
    recommend()
  }
})

window.addEventListener('hashchange', function (e) {
  goToPath(window.location.hash.substr(1))
})

goToPath(window.location.hash.substr(1))
