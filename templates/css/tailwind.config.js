/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["templates/css/*.{html,js}"],
  theme: {
    extend: { 
	colors : {
		bitcoin: {
			DEFAULT: '#FFA800',		   
		},
	},
    },
  },
  plugins: [
	  require('@tailwindcss/forms'),
	  require('@tailwindcss/typography'),
	  require('@tailwindcss/line-clamp'),
  ],
}
